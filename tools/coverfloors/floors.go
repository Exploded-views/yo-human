package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Floor is the minimum statement coverage of one package.
type Floor struct {
	Statements float64 `json:"statements"`
}

// MarshalJSON writes the percentage with exactly one decimal.
func (f Floor) MarshalJSON() ([]byte, error) {
	return []byte(`{"statements": ` + strconv.FormatFloat(f.Statements, 'f', 1, 64) + `}`), nil
}

// Violation is one failed rule for one package.
type Violation struct{ Pkg, Msg string }

// FloorsFile is the parsed coverage-floors.json: a floor per import path plus
// an "exempt" map from import path to the written reason it has no floor.
type FloorsFile struct {
	Floors map[string]Floor
	Exempt map[string]string
}

// eps absorbs float error from percentage arithmetic (3/5*100 is not exact).
const eps = 1e-9

type block struct {
	pkg     string
	stmts   int
	covered bool
}

// PackageCoverage returns statement-weighted coverage per package: covered
// statements over all statements, times 100. A block counts as covered if any
// profile line for it has a count above zero (go test -race emits repeats).
func PackageCoverage(profile io.Reader) (map[string]float64, error) {
	sc := bufio.NewScanner(profile)
	if !sc.Scan() || !strings.HasPrefix(sc.Text(), "mode: ") {
		return nil, fmt.Errorf("profile has no mode: header")
	}
	blocks := map[string]*block{}
	for sc.Scan() {
		if line := sc.Text(); line != "" {
			if err := addBlock(blocks, line); err != nil {
				return nil, err
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return weigh(blocks), nil
}

// addBlock parses "file:l.c,l.c stmts count" into blocks, keyed by position.
func addBlock(blocks map[string]*block, line string) error {
	fields := strings.Fields(line)
	if len(fields) != 3 {
		return fmt.Errorf("malformed profile line %q", line)
	}
	file, _, ok := strings.Cut(fields[0], ":")
	stmts, err1 := strconv.Atoi(fields[1])
	count, err2 := strconv.Atoi(fields[2])
	if !ok || err1 != nil || err2 != nil {
		return fmt.Errorf("malformed profile line %q", line)
	}
	b := blocks[fields[0]]
	if b == nil {
		b = &block{pkg: path.Dir(file), stmts: stmts}
		blocks[fields[0]] = b
	}
	b.covered = b.covered || count > 0
	return nil
}

func weigh(blocks map[string]*block) map[string]float64 {
	total, hit := map[string]int{}, map[string]int{}
	for _, b := range blocks {
		total[b.pkg] += b.stmts
		if b.covered {
			hit[b.pkg] += b.stmts
		}
	}
	cov := map[string]float64{}
	for pkg, n := range total {
		cov[pkg] = 0
		if n > 0 {
			cov[pkg] = float64(hit[pkg]) * 100 / float64(n)
		}
	}
	return cov
}

// RoundDown truncates a percentage to one decimal.
func RoundDown(v float64) float64 { return math.Floor(v*10+eps) / 10 }

// shown formats a percentage for messages, truncated so that a value below
// its floor never prints equal to it.
func shown(v float64) string { return strconv.FormatFloat(RoundDown(v), 'f', 1, 64) }

// Check returns every rule broken by cov against floors and, when base is
// non-nil, against the floors at the merge base: a floor may not be removed or
// lowered, and a floor new since the base must lie in
// [RoundDown(current) - newFloorSlack, current], so a package cannot enter at 0.0. Output is sorted by package.
func Check(cov map[string]float64, pkgs []string, floors, base map[string]Floor) []Violation {
	var vs []Violation
	names := map[string]bool{}
	for _, p := range pkgs {
		names[p] = true
	}
	for p := range cov {
		names[p] = true
	}
	for p := range names {
		vs = append(vs, checkOne(p, cov, floors)...)
	}
	for p, b := range base {
		vs = append(vs, checkBase(p, b, floors)...)
	}
	for p, f := range floors {
		if _, old := base[p]; base != nil && !old {
			vs = append(vs, checkNew(p, f, cov)...)
		}
	}
	sort.SliceStable(vs, func(i, j int) bool { return vs[i].Pkg < vs[j].Pkg })
	return vs
}

func checkOne(p string, cov map[string]float64, floors map[string]Floor) []Violation {
	c, hasCov := cov[p]
	f, hasFloor := floors[p]
	switch {
	case !hasCov:
		return []Violation{{p, "no coverage (no tests?)"}}
	case !hasFloor:
		return []Violation{{p, "no floor; run make floors"}}
	case RoundDown(c) < f.Statements:
		return []Violation{{p, fmt.Sprintf("%s < floor %.1f", shown(c), f.Statements)}}
	}
	return nil
}

func checkBase(p string, b Floor, floors map[string]Floor) []Violation {
	f, ok := floors[p]
	switch {
	case !ok:
		return []Violation{{p, "floor removed"}}
	case f.Statements < b.Statements:
		return []Violation{{p, fmt.Sprintf("floor lowered %.1f → %.1f", b.Statements, f.Statements)}}
	}
	return nil
}

// newFloorSlack is how far below current coverage (rounded down) a new floor
// may sit: CI on ubuntu and a local run can measure a package slightly apart.
const newFloorSlack = 0.5

// checkNew holds a floor added since the base to the window
// [RoundDown(current) - newFloorSlack, current]. A package without coverage is
// already reported by checkOne.
func checkNew(p string, f Floor, cov map[string]float64) []Violation {
	c, ok := cov[p]
	if !ok {
		return nil
	}
	lo := RoundDown(c) - newFloorSlack
	if f.Statements < lo-eps || f.Statements > c+eps {
		return []Violation{{p, fmt.Sprintf("new floor %.1f outside [%.1f, %s]", f.Statements, lo, shown(c))}}
	}
	return nil
}

// CheckExemptions fails every exemption missing from the base floors file:
// exempting a package removes its floor, which only an amendment may do.
func CheckExemptions(exempt, base map[string]string) []Violation {
	var vs []Violation
	for p := range exempt {
		if _, ok := base[p]; !ok {
			vs = append(vs, Violation{p, "exemption added: needs a constitution amendment"})
		}
	}
	sort.Slice(vs, func(i, j int) bool { return vs[i].Pkg < vs[j].Pkg })
	return vs
}

// Update returns floors for pkgs: current coverage rounded down, but never
// below an existing floor. Packages outside pkgs keep their old floor.
func Update(cov map[string]float64, pkgs []string, old map[string]Floor) map[string]Floor {
	out := map[string]Floor{}
	for p, f := range old {
		out[p] = f
	}
	for _, p := range pkgs {
		if c, ok := cov[p]; ok {
			out[p] = Floor{Statements: math.Max(RoundDown(c), old[p].Statements)}
		}
	}
	return out
}

// ApplyExemptions validates the exempt map and returns cov and pkgs without
// the exempt packages. An exemption needs a written reason and may never name
// an internal/ package: internal code always carries a floor.
func ApplyExemptions(exempt map[string]string, cov map[string]float64, pkgs []string) (map[string]float64, []string, []Violation) {
	var vs []Violation
	for p, reason := range exempt {
		switch {
		case strings.Contains(p, "/internal/"):
			vs = append(vs, Violation{p, "exemption not allowed for an internal package"})
		case strings.TrimSpace(reason) == "":
			vs = append(vs, Violation{p, "exemption needs a written reason"})
		}
	}
	sort.Slice(vs, func(i, j int) bool { return vs[i].Pkg < vs[j].Pkg })
	outCov := map[string]float64{}
	for p, c := range cov {
		if _, skip := exempt[p]; !skip || strings.Contains(p, "/internal/") {
			outCov[p] = c
		}
	}
	var outPkgs []string
	for _, p := range pkgs {
		if _, skip := exempt[p]; !skip || strings.Contains(p, "/internal/") {
			outPkgs = append(outPkgs, p)
		}
	}
	return outCov, outPkgs, vs
}

// ParseFloors reads coverage-floors.json.
func ParseFloors(r io.Reader) (FloorsFile, error) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return FloorsFile{}, err
	}
	f := FloorsFile{Floors: map[string]Floor{}, Exempt: map[string]string{}}
	for k, v := range raw {
		if k == "exempt" {
			if err := json.Unmarshal(v, &f.Exempt); err != nil {
				return FloorsFile{}, fmt.Errorf("exempt: %w", err)
			}
			continue
		}
		var fl Floor
		if err := json.Unmarshal(v, &fl); err != nil {
			return FloorsFile{}, fmt.Errorf("%s: %w", k, err)
		}
		f.Floors[k] = fl
	}
	return f, nil
}

// EncodeFloors writes f with sorted keys, one decimal per floor.
func EncodeFloors(w io.Writer, f FloorsFile) error {
	out := map[string]any{}
	for k, v := range f.Floors {
		out[k] = v
	}
	if len(f.Exempt) > 0 {
		out["exempt"] = f.Exempt
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(bytes.TrimSpace(b), '\n'))
	return err
}
