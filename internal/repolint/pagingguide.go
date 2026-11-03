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
// token, and a bare one-word cell, that is not a level is a finding. The one exception is a cell
// that starts with `yo-human (a command, such as a resolve).
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
			for _, name := range levelTokens(cells[col]) {
				flag(l.n, name)
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
	t = strings.TrimSuffix(strings.TrimPrefix(t, "|"), "|")
	var cells []string
	start := 0
	for i := 0; i < len(t); i++ {
		switch t[i] {
		case '\\':
			i++ // an escaped pipe (or any escaped character) does not split
		case '|':
			cells = append(cells, strings.TrimSpace(t[start:i]))
			start = i + 1
		}
	}
	return append(cells, strings.TrimSpace(t[start:])), true
}

// levelTokens returns what a cell of the Example level column claims is a level name: every
// backticked token, or the whole cell when it has no backticks and is a single word. A cell that
// starts with `yo-human is a command and claims nothing; a separator cell (---) claims nothing.
func levelTokens(cell string) []string {
	switch {
	case strings.HasPrefix(cell, "`yo-human"):
		return nil
	case strings.Contains(cell, "`"):
		var out []string
		for _, m := range backticks.FindAllStringSubmatch(cell, -1) {
			out = append(out, m[1])
		}
		return out
	case cell == "" || strings.ContainsAny(cell, " \t") || strings.Trim(cell, "-:") == "":
		return nil
	}
	return []string{cell}
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
