package repolint

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// The setup-page rules. A setup page is read top to bottom by a person or an agent, so it opens
// with the values the reader must supply (Inputs) and the state the page assumes (Already set),
// and every later command uses those inputs by name instead of asking for them again.
const (
	ruleInputsFirst   = "inputs-first"
	ruleInputFormat   = "input-format"
	ruleAlreadySet    = "already-set-second"
	ruleInputUnused   = "input-unused"
	ruleReassigned    = "input-reassigned"
	ruleTilde         = "tilde-home"
	ruleOpUnpiped     = "op-unpiped"
	inputsHeading     = "## Inputs"
	alreadySetHeading = "## Already set"
)

var (
	inputBullet = regexp.MustCompile("^- `([A-Z][A-Z0-9_]*)=\"<[^>]+>\"` — .+")
	// dataLangs are the fence languages that are not shell: the tilde rule skips them. Every other
	// fence, and every indented block, is read as a command someone will paste.
	dataLangs   = map[string]bool{"yaml": true, "yml": true, "json": true, "md": true, "markdown": true, "toml": true}
	bulletLine  = regexp.MustCompile(`^\s*[-*+]\s`)
	cdHome      = regexp.MustCompile(`(^|[\s;&|(])cd\s+~(\s|$|;|&|\|)`)
	opStage     = regexp.MustCompile(`^\s*op\s+read\s`)
	secretStage = regexp.MustCompile(`^\s*yo-human secret set\s+\S`)
)

// line is one line of a page with its 1-based number and where it sits.
type line struct {
	n        int
	text     string
	fenced   bool   // inside a code fence, fence lines included
	lang     string // the fence's language: the first word of its info string, lower-cased
	indented bool   // a 4-space or tab indented line outside a fence
}

// CheckSetupPage lints one setup page. name is only used to label the findings.
func CheckSetupPage(name string, md []byte) []Finding {
	c := &pageCheck{name: name, lines: scan(string(md))}
	c.headings()
	c.tilde()
	c.opLines()
	return c.found
}

type pageCheck struct {
	name  string
	lines []line
	found []Finding
}

func (c *pageCheck) add(l line, rule, format string, args ...any) {
	c.found = append(c.found, Finding{Path: c.name, Line: l.n, Rule: rule, Msg: fmt.Sprintf(format, args...)})
}

// fence describes an open code fence.
type fence struct {
	char byte
	size int
	lang string
}

// openFence reports whether text opens a fence: three or more backticks or tildes, indented by at
// most three spaces. A backtick fence's info string holds no backtick (that would be inline code).
func openFence(text string) (fence, bool) {
	t := strings.TrimLeft(text, " ")
	if len(text)-len(t) > 3 || len(t) < 3 || (t[0] != '`' && t[0] != '~') {
		return fence{}, false
	}
	size := len(t) - len(strings.TrimLeft(t, string(t[0])))
	if size < 3 {
		return fence{}, false
	}
	info := strings.TrimSpace(t[size:])
	if t[0] == '`' && strings.Contains(info, "`") {
		return fence{}, false
	}
	lang := ""
	if f := strings.Fields(info); len(f) > 0 {
		lang = strings.ToLower(f[0])
	}
	return fence{t[0], size, lang}, true
}

// closesFence reports whether text closes f: the same character, at least as many, nothing after.
func closesFence(text string, f fence) bool {
	t := strings.TrimSpace(text)
	return len(t) >= f.size && strings.Trim(t, string(f.char)) == ""
}

// scan splits md into lines, marking those inside fences and those in indented blocks. The fence
// lines themselves count as fenced.
func scan(md string) []line {
	var out []line
	var open *fence
	for i, text := range strings.Split(md, "\n") {
		l := line{n: i + 1, text: text}
		switch f, isOpen := openFence(text); {
		case open != nil:
			l.fenced, l.lang = true, open.lang
			if closesFence(text, *open) {
				open = nil
			}
		case isOpen:
			open = &f
			l.fenced, l.lang = true, f.lang
		default:
			l.indented = strings.HasPrefix(text, "    ") || strings.HasPrefix(text, "\t")
		}
		out = append(out, l)
	}
	return out
}

// h2s returns the indexes of the "## " headings outside code fences.
func (c *pageCheck) h2s() []int {
	var idx []int
	for i, l := range c.lines {
		if !l.fenced && strings.HasPrefix(l.text, "## ") {
			idx = append(idx, i)
		}
	}
	return idx
}

// headings checks the first two level-2 headings, the shape of the Inputs bullets and the use of
// each input later in the page.
func (c *pageCheck) headings() {
	h := c.h2s()
	if len(h) == 0 || strings.TrimSpace(c.lines[h[0]].text) != inputsHeading {
		at := line{n: 1}
		if len(h) > 0 {
			at = c.lines[h[0]]
		}
		c.add(at, ruleInputsFirst, "the first ## heading must be %q", inputsHeading)
		return
	}
	end := len(c.lines)
	if len(h) > 1 {
		end = h[1]
		if strings.TrimSpace(c.lines[h[1]].text) != alreadySetHeading {
			c.add(c.lines[h[1]], ruleAlreadySet, "the second ## heading must be %q", alreadySetHeading)
		}
	} else {
		c.add(c.lines[h[0]], ruleAlreadySet, "the second ## heading must be %q, and there is none", alreadySetHeading)
	}
	names := c.inputs(h[0]+1, end)
	c.usage(names, end)
}

// inputs checks each bullet between start and end and returns the names of the valid ones, with
// the line each was declared on. A bullet is "- " at column 0; "*", "+" and indented bullets are
// rejected, and the section needs at least one valid bullet.
func (c *pageCheck) inputs(start, end int) map[string]line {
	names, bad := map[string]line{}, 0
	for _, l := range c.lines[start:end] {
		if l.fenced || !bulletLine.MatchString(l.text) {
			continue
		}
		m := inputBullet.FindStringSubmatch(l.text)
		if m == nil {
			bad++
			c.add(l, ruleInputFormat, "an Inputs bullet must start at column 0 and look like: - `NAME=\"<placeholder>\"` — what it is")
			continue
		}
		names[m[1]] = l
	}
	if len(names) == 0 && bad == 0 {
		c.add(c.lines[start-1], ruleInputFormat, "the Inputs section needs at least one bullet")
	}
	return names
}

// usage checks that every input is read as $NAME or ${NAME} after the Inputs section and never
// assigned again there.
func (c *pageCheck) usage(names map[string]line, from int) {
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	for _, name := range sorted {
		decl := names[name]
		use := regexp.MustCompile(`\$\{?` + name + `\b`)
		assign := regexp.MustCompile(`(^|[\s;&|(])` + name + `\+?=|(^|[\s;&|(])(read|set)\s+(-\S+\s+)*` + name + `\b`)
		used := false
		for _, l := range c.lines[from:] {
			if use.MatchString(l.text) {
				used = true
			}
			if assign.MatchString(l.text) {
				c.add(l, ruleReassigned, "%s is an input: use $%s, do not assign it again", name, name)
			}
		}
		if !used {
			c.add(decl, ruleInputUnused, "%s is declared under Inputs but never used later as $%s", name, name)
		}
	}
}

// tilde flags "~/" and "cd ~" in every fence except the data languages, and in indented blocks:
// a path under the home directory is the reader's, not the page's.
func (c *pageCheck) tilde() {
	for _, l := range c.lines {
		if (!l.fenced || dataLangs[l.lang]) && !l.indented {
			continue
		}
		if strings.Contains(l.text, "~/") || cdHome.MatchString(l.text) {
			c.add(l, ruleTilde, "no ~/ or cd ~ in a code block: use $HOME, or $XDG_CONFIG_HOME for yo-human's own files")
		}
	}
}

// logical joins the physical lines from i that one shell command spans: a trailing backslash, pipe
// or || continues onto the next line. It returns the command and the index of its last line.
func (c *pageCheck) logical(i int) (string, int) {
	cmd, j := c.lines[i].text, i
	for j+1 < len(c.lines) {
		t := strings.TrimRight(cmd, " \t")
		switch {
		case strings.HasSuffix(t, `\`):
			t = strings.TrimSuffix(t, `\`)
		case !strings.HasSuffix(t, "|"):
			return cmd, j
		}
		j++
		cmd = t + " " + strings.TrimSpace(c.lines[j].text)
	}
	return cmd, j
}

// opLines flags an op:// reference that is not the left side of a pipe into yo-human secret set:
// the stage holding it must be a plain `op read ...` (no ; && $( ` > around it) and the next stage
// must be `yo-human secret set <name>`. An `||` splits into an empty stage, which is not one.
func (c *pageCheck) opLines() {
	for i := 0; i < len(c.lines); i++ {
		cmd, last := c.logical(i)
		if strings.Contains(cmd, "op://") && !pipesIntoSecretSet(cmd) {
			c.add(c.lines[i], ruleOpUnpiped, "an op:// line must pipe straight into `yo-human secret set <name>`: a secret never reaches a variable or a screen")
		}
		i = last
	}
}

// pipesIntoSecretSet reports whether every stage of cmd that holds op:// is an `op read` whose
// output feeds `yo-human secret set` as the next stage.
func pipesIntoSecretSet(cmd string) bool {
	stages := strings.Split(cmd, "|")
	for k, st := range stages {
		if !strings.Contains(st, "op://") {
			continue
		}
		if !opStage.MatchString(st) || strings.ContainsAny(st, ";&`><") || strings.Contains(st, "$(") {
			return false
		}
		if k+1 >= len(stages) || !secretStage.MatchString(stages[k+1]) {
			return false
		}
	}
	return true
}
