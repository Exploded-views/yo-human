package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// jsonVersion is the "version" of every --json object.
const jsonVersion = 1

// oneLine keeps a value on its own line and free of control, format (BiDi overrides, joiners) and
// line-separator characters (a terminal would act on an escape sequence), so the error: / valid: / fix: shape cannot be forged.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, s)
}

// jsonResult is the spec's result object, with the problems that came with it, and the notices and
// warnings the run produced.
type jsonResult struct {
	Version int `json:"version"`
	core.Result
	Errors   []jsonProblem `json:"errors,omitempty"`
	Notices  []string      `json:"notices,omitempty"`
	Warnings []jsonProblem `json:"warnings,omitempty"`
}

// writeResult prints a command's result: the RESULT line first, then one line per channel and the
// next step; or, with asJSON, the spec's object. It never prints a secret: a ChannelResult holds
// only the channel's own error text.
func writeResult(stdout io.Writer, r core.Result, asJSON bool) {
	if asJSON {
		writeJSONResult(stdout, r, nil, nil, nil)
		return
	}
	head := "RESULT " + string(r.Status)
	if r.ID != "" {
		head += " " + r.ID
	}
	_, _ = fmt.Fprintln(stdout, head)
	for _, c := range r.Channels {
		line := c.Channel + ": " + c.Status
		if c.Error != "" {
			line += " (" + oneLine(c.Error) + ")"
		}
		_, _ = fmt.Fprintln(stdout, line)
	}
	if r.DedupKey != "" {
		_, _ = fmt.Fprintln(stdout, "key: "+oneLine(r.DedupKey))
	}
	if n := r.Next; n != nil {
		_, _ = fmt.Fprintf(stdout, "next: %s in %ds, %d attempts left, then %s\n", oneLine(n.Do), n.AfterSeconds, n.AttemptsLeft, oneLine(n.Then))
	}
}

func writeJSONResult(w io.Writer, r core.Result, ps []*problem.Problem, notes []string, warns []*problem.Problem) {
	if r.Channels == nil {
		r.Channels = []core.ChannelResult{}
	}
	obj := jsonResult{Version: jsonVersion, Result: r, Notices: notes, Warnings: toWarningList(warns)}
	obj.Errors = toJSONList(ps)
	writeJSON(w, obj)
}

// toWarningList is toJSONList for warnings, without the "warning: " prefix their text carries.
func toWarningList(ps []*problem.Problem) []jsonProblem {
	out := toJSONList(ps)
	for i := range out {
		out[i].What = strings.TrimPrefix(out[i].What, "warning: ")
	}
	return out
}

func toJSONList(ps []*problem.Problem) []jsonProblem {
	var out []jsonProblem
	for _, p := range ps {
		out = append(out, toJSON(p))
	}
	return out
}

// writeOK prints a read-only command's output: RESULT ok, then lines as given (a caller that
// shows text from a file passes it through oneLine); or, with asJSON, an object holding fields.
func writeOK(stdout io.Writer, asJSON bool, lines []string, fields map[string]any) {
	if asJSON {
		obj := map[string]any{"version": jsonVersion, "status": string(core.StatusOK)}
		for k, v := range fields {
			obj[k] = v
		}
		writeJSON(stdout, obj)
		return
	}
	_, _ = fmt.Fprintln(stdout, "RESULT ok")
	for _, l := range lines {
		_, _ = fmt.Fprintln(stdout, l)
	}
}

func writeJSON(w io.Writer, v any) {
	b, _ := json.Marshal(v) // every value passed here is made of strings, numbers and structs of them
	_, _ = fmt.Fprintf(w, "%s\n", b)
}

type jsonProblem struct {
	What  string `json:"what"`
	Valid string `json:"valid"`
	Fix   string `json:"fix"`
}

func toJSON(p *problem.Problem) jsonProblem { return jsonProblem{p.What, p.Valid, p.Fix} }

// jsonFailure is the object for a command that produced only problems: "error" is the first and
// "errors" lists all when there are several.
type jsonFailure struct {
	Version  int           `json:"version"`
	Error    jsonProblem   `json:"error"`
	Errors   []jsonProblem `json:"errors,omitempty"`
	Notices  []string      `json:"notices,omitempty"`
	Warnings []jsonProblem `json:"warnings,omitempty"`
}

// writeProblem prints one problem and returns its exit code.
func writeProblem(stderr io.Writer, p *problem.Problem, asJSON bool) int {
	return writeProblems(stderr, p, asJSON)
}

// writeProblems prints every problem in err, one error: / valid: / fix: block each (or, with
// asJSON, one object on w). The exit code is the first problem's. An error that is not a Problem
// is a bug and exits 70.
func writeProblems(w io.Writer, err error, asJSON bool) int {
	return writeFailure(w, flatten(err), asJSON, nil, nil)
}

func writeFailure(w io.Writer, ps []*problem.Problem, asJSON bool, notes []string, warns []*problem.Problem) int {
	if asJSON {
		obj := jsonFailure{Version: jsonVersion, Error: toJSON(ps[0]), Notices: notes, Warnings: toWarningList(warns)}
		if len(ps) > 1 {
			obj.Errors = toJSONList(ps)
		}
		writeJSON(w, obj)
		return exitForProblem(ps[0])
	}
	for _, p := range ps {
		_, _ = fmt.Fprintf(w, "error: %s\nvalid: %s\nfix: %s\n", oneLine(p.What), oneLine(p.Valid), oneLine(p.Fix))
	}
	return exitForProblem(ps[0])
}

// flatten lists the problems in err, looking inside errors.Join. It always returns at least one.
func flatten(err error) []*problem.Problem {
	var joined interface{ Unwrap() []error }
	if errors.As(err, &joined) {
		var out []*problem.Problem
		for _, e := range joined.Unwrap() {
			out = append(out, flatten(e)...)
		}
		return out
	}
	if p, ok := problem.As(err); ok {
		return []*problem.Problem{p}
	}
	return []*problem.Problem{problem.Internalf("unexpected error: %s", "a Problem with what, valid and fix", "report this as a bug", err.Error())}
}

// writeWarnings prints advice that does not fail the command.
func writeWarnings(stderr io.Writer, ps []*problem.Problem) {
	for _, p := range ps {
		_, _ = fmt.Fprintf(stderr, "warning: %s\nvalid: %s\nfix: %s\n", oneLine(strings.TrimPrefix(p.What, "warning: ")), oneLine(p.Valid), oneLine(p.Fix))
	}
}

// writeNotices prints the loader's plain notices.
func writeNotices(stderr io.Writer, notes []string) {
	for _, n := range notes {
		_, _ = fmt.Fprintln(stderr, "notice: "+oneLine(n))
	}
}
