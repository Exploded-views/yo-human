package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
)

var sample = core.Result{
	Status: core.StatusQueued, ID: "01ABC",
	Channels: []core.ChannelResult{{Channel: "phone", Status: "queued", Error: "timed out\nsecond line"}, {Channel: "journal", Status: "sent"}},
	Next:     &core.Next{Do: "yo-human flush", AfterSeconds: 60, AttemptsLeft: 5, Then: "tell the human"},
}

func TestWriteResultText(t *testing.T) {
	var b bytes.Buffer
	writeResult(&b, sample, false)
	want := "RESULT queued 01ABC\nphone: queued (timed out second line)\njournal: sent\nnext: yo-human flush in 60s, 5 attempts left, then tell the human\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
	b.Reset()
	writeResult(&b, core.Result{Status: core.StatusNoop}, false)
	if b.String() != "RESULT noop\n" {
		t.Errorf("no id: got %q, want %q", b.String(), "RESULT noop\n")
	}
}

func TestWriteResultJSON(t *testing.T) {
	var b bytes.Buffer
	writeResult(&b, sample, true)
	var got struct {
		Version  int
		Status   string
		ID       string
		Channels []struct{ Channel, Status, Error string }
		Next     struct {
			Do           string
			AfterSeconds int `json:"after_seconds"`
			AttemptsLeft int `json:"attempts_left"`
		}
	}
	if err := json.Unmarshal(b.Bytes(), &got); err != nil || got.Version != 1 || got.Status != "queued" || got.ID != "01ABC" ||
		len(got.Channels) != 2 || got.Next.AfterSeconds != 60 || got.Next.AttemptsLeft != 5 {
		t.Errorf("got %q (%v); want the spec's object with version 1", b.String(), err)
	}
}

func TestWriteProblemsRendersEveryProblemInAJoin(t *testing.T) {
	err := errors.Join(
		problem.Configf("first %s", "valid one", "fix one", "went wrong"),
		fmt.Errorf("wrapped: %w", errors.Join(problem.Usagef("second", "valid two", "fix two"), problem.Storef("third", "valid three", "fix three"))),
	)
	var b bytes.Buffer
	code := writeProblems(&b, err, false)
	want := "error: first went wrong\nvalid: valid one\nfix: fix one\n" +
		"error: second\nvalid: valid two\nfix: fix two\n" +
		"error: third\nvalid: valid three\nfix: fix three\n"
	if code != ExitConfig || b.String() != want {
		t.Errorf("code=%d got %q; want exit 78 (the first problem's) and %q", code, b.String(), want)
	}

	b.Reset()
	code = writeProblems(&b, err, true)
	var obj struct {
		Version int
		Error   struct{ What string }
		Errors  []struct{ What, Valid, Fix string }
	}
	if e := json.Unmarshal(b.Bytes(), &obj); e != nil || code != ExitConfig || obj.Error.What != "first went wrong" || len(obj.Errors) != 3 || obj.Errors[2].Fix != "fix three" {
		t.Errorf("json: code=%d got %q (%v)", code, b.String(), e)
	}
	b.Reset()
	writeProblems(&b, problem.Usagef("only", "v", "f"), true)
	mustNotContain(t, "single problem json", b.String(), `"errors"`)
}

func TestWriteProblemKeepsThreeLines(t *testing.T) {
	var b bytes.Buffer
	code := writeProblem(&b, problem.Usagef("what\nforged: line", "valid\r\nmore", "fix"), false)
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if code != ExitUsage || len(lines) != 3 || !strings.HasPrefix(lines[0], "error: ") || !strings.HasPrefix(lines[1], "valid: ") || !strings.HasPrefix(lines[2], "fix: ") {
		t.Errorf("code=%d got %q; want exactly three lines", code, b.String())
	}
}

func TestWriteProblemsTreatsAPlainErrorAsInternal(t *testing.T) {
	var b bytes.Buffer
	if code := writeProblems(&b, errors.New("disk on fire"), false); code != ExitInternal || !strings.HasPrefix(b.String(), "error: unexpected error: disk on fire\n") {
		t.Errorf("code=%d got %q; want exit 70", code, b.String())
	}
}

func TestWriteWarningsAndNotices(t *testing.T) {
	var b bytes.Buffer
	writeWarnings(&b, []*problem.Problem{problem.Configf("warning: level %q is loud", "v", "f", "page"), problem.Configf("plain", "v2", "f2")})
	want := "warning: level \"page\" is loud\nvalid: v\nfix: f\nwarning: plain\nvalid: v2\nfix: f2\n"
	if b.String() != want {
		t.Errorf("warnings: got %q, want %q", b.String(), want)
	}
	b.Reset()
	writeNotices(&b, []string{"ignoring the project config", "second\nline"})
	if b.String() != "notice: ignoring the project config\nnotice: second line\n" {
		t.Errorf("notices: got %q", b.String())
	}
}

func TestWriteOKTextAndJSON(t *testing.T) {
	var b bytes.Buffer
	writeOK(&b, false, []string{"one", "two\nthree"}, map[string]any{"x": 1})
	if b.String() != "RESULT ok\none\ntwo\nthree\n" {
		t.Errorf("text: got %q", b.String())
	}
	b.Reset()
	writeOK(&b, true, []string{"ignored"}, map[string]any{"x": 1})
	if b.String() != `{"status":"ok","version":1,"x":1}`+"\n" {
		t.Errorf("json: got %q", b.String())
	}
}

func TestFinishPrintsTheResultThenTheProblemsAndExitsWithTheProblem(t *testing.T) {
	q := core.Result{Status: core.StatusQueued, ID: "01ABC"}
	for _, c := range []struct {
		name string
		r    *core.Result
		err  error
		want int
		out  string
	}{
		{"result only, queued", &q, nil, ExitQueued, "RESULT queued 01ABC\n"},
		{"result only, delivered", &core.Result{Status: core.StatusDelivered, ID: "x"}, nil, ExitOK, "RESULT delivered x\n"},
		{"result and a store problem", &q, problem.Storef("disk", "v", "f"), ExitStore, "RESULT queued 01ABC\n"},
		{"result and a config problem", &q, problem.Configf("cfg", "v", "f"), ExitConfig, "RESULT queued 01ABC\n"},
		{"problem only", nil, problem.Usagef("bad", "v", "f"), ExitUsage, ""},
	} {
		var out, errb bytes.Buffer
		e := &env{stdout: &out, stderr: &errb}
		code := e.finish(c.r, c.err)
		if code != c.want || out.String() != c.out || (c.err != nil) != strings.HasPrefix(errb.String(), "error: ") {
			t.Errorf("%s: code=%d out=%q err=%q; want %d and %q", c.name, code, out.String(), errb.String(), c.want, c.out)
		}
	}
}

func TestWriteResultJSONNormalisesNilChannels(t *testing.T) {
	var b bytes.Buffer
	writeResult(&b, core.Result{Status: core.StatusNoop}, true)
	mustContain(t, "json", b.String(), `"channels":[]`)
}

// Under --json stdout carries exactly one object and stderr stays empty.
func TestFinishJSONPutsProblemsInTheResultObject(t *testing.T) {
	var out, errb bytes.Buffer
	e := &env{stdout: &out, stderr: &errb, asJSON: true, notes: []string{"a note"}}
	code := e.finish(&core.Result{Status: core.StatusQueued, ID: "01ABC"}, errors.Join(problem.Storef("disk", "v", "f"), problem.Configf("cfg", "v2", "f2")))
	var obj struct {
		Status  string
		ID      string
		Errors  []struct{ What string }
		Notices []string
	}
	err := json.Unmarshal(out.Bytes(), &obj)
	expect(t, err == nil && code == ExitStore && errb.Len() == 0, "code=%d out=%q err=%q (%v); want one object, stderr empty, exit 74", code, out.String(), errb.String(), err)
	expect(t, obj.Status == "queued" && obj.ID == "01ABC" && len(obj.Errors) == 2 && obj.Errors[1].What == "cfg" && len(obj.Notices) == 1, "object = %+v", obj)
	expect(t, strings.Count(strings.TrimSpace(out.String()), "\n") == 0, "stdout holds more than one line: %q", out.String())
}

func TestFailJSONPutsTheErrorObjectOnStdout(t *testing.T) {
	var out, errb bytes.Buffer
	e := &env{stdout: &out, stderr: &errb, asJSON: true, warns: []*problem.Problem{problem.Configf("warning: w", "v", "f")}}
	code := e.fail(errors.Join(problem.Configf("one", "v", "f"), problem.Usagef("two", "v", "f")))
	var obj struct {
		Error    struct{ What string }
		Errors   []struct{ What string }
		Warnings []struct{ What, Valid, Fix string }
	}
	err := json.Unmarshal(out.Bytes(), &obj)
	expect(t, err == nil && code == ExitConfig && errb.Len() == 0, "code=%d out=%q err=%q (%v)", code, out.String(), errb.String(), err)
	expect(t, obj.Error.What == "one" && len(obj.Errors) == 2 && len(obj.Warnings) == 1 && obj.Warnings[0].Fix == "f", "object = %+v", obj)
}

func TestFinishJSONWithoutProblemsExitsWithTheResult(t *testing.T) {
	var out, errb bytes.Buffer
	e := &env{stdout: &out, stderr: &errb, asJSON: true}
	if code := e.finish(&core.Result{Status: core.StatusQueued, ID: "x"}, nil); code != ExitQueued || errb.Len() != 0 {
		t.Errorf("code=%d err=%q, want 75 and an empty stderr", code, errb.String())
	}
	mustNotContain(t, "json", out.String(), "errors")
}
