package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// Budget is one scenario's p95 wall-time ceiling.
type Budget struct {
	P95Ms int `json:"p95_ms"`
}

// Budgets is perf-budgets.json (crewkit's schema).
type Budgets struct {
	Runs       int               `json:"runs"`
	CIHeadroom float64           `json:"ci_headroom"`
	Budgets    map[string]Budget `json:"budgets"`
}

// Violation is one failed check, printed as "scenario: message".
type Violation struct {
	Scenario string
	Message  string
}

func (v Violation) String() string { return v.Scenario + ": " + v.Message }

// ReadBudgets loads and validates a budgets file.
func ReadBudgets(path string) (Budgets, error) {
	var b Budgets
	raw, err := os.ReadFile(path)
	if err != nil {
		return b, err
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return b, fmt.Errorf("%s: %w", path, err)
	}
	if b.Runs < 1 || b.CIHeadroom < 1 {
		return b, fmt.Errorf("%s: need runs >= 1 and ci_headroom >= 1.0", path)
	}
	for _, name := range sortedKeys(b.Budgets) {
		if b.Budgets[name].P95Ms <= 0 {
			return b, fmt.Errorf("%s: %s: p95_ms must be > 0", path, name)
		}
	}
	return b, nil
}

// P95 is the nearest-rank 95th percentile: index ceil(0.95*n)-1 of the sorted samples.
func P95(samples []time.Duration) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), samples...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[(len(s)*95+99)/100-1]
}

// Check compares each scenario's p95 with its budget. Headroom multiplies the
// budget only when ci is true. Scenarios and budgets must match one to one.
func Check(results map[string][]time.Duration, b Budgets, ci bool) []Violation {
	var vs []Violation
	for _, name := range sortedKeys(results) {
		bud, ok := b.Budgets[name]
		if !ok {
			vs = append(vs, Violation{name, "no budget in perf-budgets.json"})
			continue
		}
		limit := time.Duration(bud.P95Ms) * time.Millisecond
		note := ""
		if ci {
			limit = time.Duration(float64(limit) * b.CIHeadroom)
			note = fmt.Sprintf(" (%dms x %.1f CI headroom)", bud.P95Ms, b.CIHeadroom)
		}
		if p := P95(results[name]); p > limit {
			vs = append(vs, Violation{name, fmt.Sprintf("p95 %s > budget %s%s", ms(p), ms(limit), note)})
		}
	}
	for _, name := range sortedKeys(b.Budgets) {
		if _, ok := results[name]; !ok {
			vs = append(vs, Violation{name, "no scenario for this budget"})
		}
	}
	return vs
}

func ms(d time.Duration) string { return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond)) }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// CheckBase fails when cur loosens base: a raised budget or ci_headroom, fewer
// runs, or a removed budget. Tightening and new scenarios pass.
func CheckBase(cur, base Budgets) []Violation {
	var vs []Violation
	for _, name := range sortedKeys(base.Budgets) {
		now, ok := cur.Budgets[name]
		old := base.Budgets[name].P95Ms
		switch {
		case !ok:
			vs = append(vs, Violation{name, "budget removed"})
		case now.P95Ms > old:
			vs = append(vs, Violation{name, fmt.Sprintf("budget raised %dms → %dms", old, now.P95Ms)})
		}
	}
	if cur.CIHeadroom > base.CIHeadroom {
		vs = append(vs, Violation{"ci_headroom", fmt.Sprintf("raised %.1f → %.1f", base.CIHeadroom, cur.CIHeadroom)})
	}
	if cur.Runs < base.Runs {
		vs = append(vs, Violation{"runs", fmt.Sprintf("lowered %d → %d", base.Runs, cur.Runs)})
	}
	return vs
}
