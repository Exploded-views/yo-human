// Command coverfloors enforces per-package coverage floors that only rise.
//
// Exit codes: 0 clean, 1 violations, 2 usage or unreadable input.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

type options struct {
	profile, packages, floors, baseFloors string
	update                                bool
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	var o options
	fs := flag.NewFlagSet("coverfloors", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.profile, "profile", "", "go test -coverprofile output (required)")
	fs.StringVar(&o.packages, "packages", "", "file with one import path per line, from go list ./... (required)")
	fs.StringVar(&o.floors, "floors", "coverage-floors.json", "floors file")
	fs.StringVar(&o.baseFloors, "base-floors", "", "floors file from the merge base")
	fs.BoolVar(&o.update, "update", false, "raise floors to current coverage and write the floors file")
	if err := fs.Parse(args); err != nil || o.profile == "" || o.packages == "" {
		_, _ = fmt.Fprintln(stderr, "usage: coverfloors --profile F --packages F [--floors F] [--base-floors F] [--update]")
		return 2
	}
	code, err := execute(o, stdout)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "coverfloors:", err)
		return 2
	}
	return code
}

func execute(o options, stdout io.Writer) (int, error) {
	cov, pkgs, ff, err := load(o)
	if err != nil {
		return 2, err
	}
	cov, pkgs, vs := ApplyExemptions(ff.Exempt, cov, pkgs)
	pkgs, err = DropStatementless(cov, pkgs, moduleDir)
	if err != nil {
		return 2, err
	}
	if o.update && len(vs) == 0 {
		return writeUpdate(o, cov, pkgs, ff, stdout)
	}
	var base map[string]Floor
	if o.baseFloors != "" {
		bf, err := readFloors(o.baseFloors, false)
		if err != nil {
			return 2, err
		}
		base = bf.Floors
		vs = append(vs, CheckExemptions(ff.Exempt, bf.Exempt)...)
	}
	return report(stdout, append(vs, Check(cov, pkgs, ff.Floors, base)...)), nil
}

func load(o options) (map[string]float64, []string, FloorsFile, error) {
	var ff FloorsFile
	pf, err := os.Open(o.profile)
	if err != nil {
		return nil, nil, ff, err
	}
	defer func() { _ = pf.Close() }()
	cov, err := PackageCoverage(pf)
	if err != nil {
		return nil, nil, ff, fmt.Errorf("%s: %w", o.profile, err)
	}
	raw, err := os.ReadFile(o.packages)
	if err != nil {
		return nil, nil, ff, err
	}
	ff, err = readFloors(o.floors, o.update)
	return cov, strings.Fields(string(raw)), ff, err
}

// readFloors loads a floors file; a missing file is empty only when allowed.
func readFloors(file string, missingOK bool) (FloorsFile, error) {
	f, err := os.Open(file)
	if os.IsNotExist(err) && missingOK {
		return FloorsFile{Floors: map[string]Floor{}, Exempt: map[string]string{}}, nil
	}
	if err != nil {
		return FloorsFile{}, err
	}
	defer func() { _ = f.Close() }()
	ff, err := ParseFloors(f)
	if err != nil {
		return ff, fmt.Errorf("%s: %w", file, err)
	}
	return ff, nil
}

func writeUpdate(o options, cov map[string]float64, pkgs []string, ff FloorsFile, stdout io.Writer) (int, error) {
	// A package without coverage gets no floor, so the update would be incomplete.
	if vs := Check(cov, pkgs, Update(cov, pkgs, ff.Floors), nil); len(vs) > 0 {
		return report(stdout, vs), nil
	}
	ff.Floors = Update(cov, pkgs, ff.Floors)
	var buf bytes.Buffer
	if err := EncodeFloors(&buf, ff); err != nil {
		return 2, err
	}
	return 0, os.WriteFile(o.floors, buf.Bytes(), 0o644) //nolint:gosec // tracked repo file
}

func report(w io.Writer, vs []Violation) int {
	for _, v := range vs {
		_, _ = fmt.Fprintf(w, "%s: %s\n", v.Pkg, v.Msg)
	}
	if len(vs) > 0 {
		return 1
	}
	return 0
}
