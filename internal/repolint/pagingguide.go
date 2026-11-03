package repolint

import (
	"fmt"
	"regexp"
	"strings"
)

const ruleUnknownLevel = "unknown-level"

var (
	levelFlag = regexp.MustCompile(`--level(?:=|\s+)["']?([A-Za-z0-9_-]+)`)
	backticks = regexp.MustCompile("`([^`]+)`")
)

// CheckLevelNames returns one finding per level name that page uses and levels does not list. A
// page uses a level name in two places: a backticked token in the "Example level" column of a
// markdown table, and the argument of --level (or --level=) anywhere, fenced or not. A backticked
// token with a space in it is a command, not a level name, and is skipped.
func CheckLevelNames(page string, levels []string) []Finding {
	known := map[string]bool{}
	for _, l := range levels {
		known[l] = true
	}
	var out []Finding
	flag := func(n int, name string) {
		if !known[name] {
			out = append(out, Finding{Line: n, Rule: ruleUnknownLevel,
				Msg: fmt.Sprintf("level %q is not in the starter config's levels %v", name, levels)})
		}
	}
	col := -1 // the Example level column of the table being read, or -1 outside one
	for _, l := range scan(page) {
		for _, m := range levelFlag.FindAllStringSubmatch(l.text, -1) {
			flag(l.n, m[1])
		}
		cells, isRow := tableRow(l)
		switch {
		case !isRow:
			col = -1
		case col < 0:
			col = exampleLevelColumn(cells)
		case col < len(cells):
			for _, m := range backticks.FindAllStringSubmatch(cells[col], -1) {
				if !strings.ContainsAny(m[1], " \t") {
					flag(l.n, m[1])
				}
			}
		}
	}
	return out
}

// tableRow splits a line outside a fence that starts with "|" into its trimmed cells.
func tableRow(l line) ([]string, bool) {
	t := strings.TrimSpace(l.text)
	if l.fenced || !strings.HasPrefix(t, "|") {
		return nil, false
	}
	cells := strings.Split(strings.Trim(t, "|"), "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells, true
}

// exampleLevelColumn returns the index of the "Example level" cell in a header row, or -1.
func exampleLevelColumn(header []string) int {
	for i, c := range header {
		if strings.EqualFold(c, "Example level") {
			return i
		}
	}
	return -1
}
