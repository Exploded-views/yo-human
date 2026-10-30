package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/Exploded-views/yo-human/internal/problem"
)

// hasControl reports a raw control byte (C0 other than newline, DEL, or C1) in s.
func hasControl(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return r != '\n' && unicode.IsControl(r) })
}

// payloads are the display-forging attempts: a newline that fakes a line, ESC sequences that
// erase the real lines on a terminal, and a C1 control.
var payloads = map[string]string{
	"newline": `x\nchannel \"office\" server \"https://trusted.example\"`,
	"escape":  `x\e[2A\e[2K\r`,
	"c1":      `x\u009b2Ay`,
}

// forged is a project file with the payload in a key (kind "key"), a string value ("value") or an
// interruption ("interruption").
func forged(kind, payload string) string {
	key, value, interruption := "server", "ok", "critical"
	switch kind {
	case "key":
		key = `"` + payload + `"`
	case "value":
		value = `"` + payload + `"`
	default:
		interruption = `"` + payload + `"`
	}
	return "version: 1\nchannels:\n  office:\n    type: ntfy\n    " + key + ": " + value + "\n" +
		"levels:\n  - { name: page, use_when: x, send: [ { channel: office, interruption: " + interruption + " } ] }\n"
}

// A control character in a key, or any control character but \n and \t in a string, is refused
// before anything is recorded, and nothing the file holds reaches stdout or stderr.
func TestConfigTrustRefusesControlCharacters(t *testing.T) {
	for name, payload := range payloads {
		for _, kind := range []string{"key", "value", "interruption"} {
			if kind != "key" && name == "newline" {
				continue // a newline in a value is allowed; see below
			}
			s := newSandbox(t)
			proj := write(t, filepath.Join(s.home, "p", ".yo-human.yaml"), forged(kind, payload))
			r := s.do("", "config", "trust", proj)
			r.fails(t, name+" in "+kind, ExitConfig, "control character")
			expect(t, !hasControl(r.out+r.err) && !strings.Contains(r.out+r.err, "trusted.example") && !strings.Contains(r.err, "2A"), "%s in %s: output %q %q holds the payload", name, kind, r.out, r.err)
			_, err := os.Stat(filepath.Join(s.home, ".local", "state", "yo-human", "trust.json"))
			expect(t, err != nil, "%s in %s: a trust record was written", name, kind)
		}
	}
}

// A newline or tab in a string value is ordinary text (a block scalar), and is escaped on display.
// An interruption is one of four words, so a newline there breaks the schema and is refused.
func TestConfigTrustAllowsNewlinesAndTabsInValues(t *testing.T) {
	for _, kind := range []string{"value", "interruption"} {
		s := newSandbox(t)
		proj := write(t, filepath.Join(s.home, "p", ".yo-human.yaml"), forged(kind, payloads["newline"]))
		r := s.do("", "config", "trust", proj)
		if kind == "interruption" {
			r.fails(t, "newline in "+kind, ExitConfig, "breaks the config schema")
		} else {
			r.succeeds(t, "newline in "+kind)
		}
		for _, line := range strings.Split(r.out+r.err, "\n") {
			expect(t, !strings.HasPrefix(line, `channel "office" server "https://trusted.example"`), "%s: a forged line appeared: %q", kind, line)
		}
	}
	s := newSandbox(t)
	body := "version: 1\nchannels:\n  office: { type: ntfy, server: \"https://ntfy.example.com\", tag: \"a\\tb\" }\n" +
		"levels:\n  - name: page\n    use_when: |\n      Production is down.\n      Wake the human.\n    send: [ { channel: office, interruption: critical } ]\n"
	r := s.do("", "config", "trust", write(t, filepath.Join(s.home, "p", ".yo-human.yaml"), body))
	r.succeeds(t, "block scalar and tab")
	mustContain(t, "trust output", r.out, `level "page" use_when "Production is down.\nWake the human.\n"`, `channel "office" tag "a\tb"`)
	expect(t, !hasControl(r.out), "raw control bytes in %q", r.out)
}

// yaml.v3 decodes a mapping with mixed key types to map[any]any; it is scanned all the same.
func TestConfigTrustScansMappingsWithMixedKeyTypes(t *testing.T) {
	s := newSandbox(t)
	body := "version: 1\nchannels:\n  office:\n    type: ntfy\n    1: a\n    \"k\\e[2K\": v\n"
	s.do("", "config", "trust", write(t, filepath.Join(s.home, "p", ".yo-human.yaml"), body)).fails(t, "mixed keys", ExitConfig, "control character")
}

func TestHoldsControl(t *testing.T) {
	for name, c := range map[string]struct {
		v    any
		want bool
	}{
		"plain":            {map[string]any{"a": "b"}, false},
		"newline in value": {map[string]any{"a": "x\ny"}, false},
		"tab in value":     {map[string]any{"a": "x\ty"}, false},
		"CR in value":      {map[string]any{"a": "x\ry"}, true},
		"ESC in value":     {[]any{"x\x1by"}, true},
		"DEL in value":     {"x\x7fy", true},
		"C1 in value":      {"x\u009by", true},
		"newline in key":   {map[string]any{"a\nb": "v"}, true},
		"tab in key":       {map[string]any{"a\tb": "v"}, true},
		"mixed keys":       {map[any]any{1: "a", "k\x1b": "v"}, true},
		"mixed, clean":     {map[any]any{1: "a", "k": "v\n"}, false},
		"mixed, bad value": {map[any]any{1: "a\x1b"}, true},
	} {
		expect(t, holdsControl(c.v) == c.want, "%s: holdsControl = %v, want %v", name, !c.want, c.want)
	}
}

func TestOneLineNeutralisesFormatAndSeparatorCharacters(t *testing.T) {
	got := oneLine("a\u202eb\u200dc\u2028d\u2029e\x1bf\u00adg")
	expect(t, got == "a b c d e f g", "oneLine = %q, want every one of them replaced by a space", got)
	s := newSandbox(t)
	write(t, s.userConfig(), strings.Replace(userConfigBody, "FYI only", "FYI \u202eevil", 1))
	r := s.do("", "levels")
	expect(t, r.code == ExitOK && !strings.ContainsRune(r.out, '\u202e'), "levels output %q holds a BiDi override", r.out)
	var b bytes.Buffer
	writeProblem(&b, problem.Usagef("bad \u202e text", "v", "f"), false)
	expect(t, !strings.ContainsRune(b.String(), '\u202e'), "problem text %q holds a BiDi override", b.String())
}

// Even a view built from such a document escapes every file-derived string, in text and JSON.
func TestDescribeEscapesEveryFileDerivedString(t *testing.T) {
	evil := "x\n\x1b[2A\x1b[2K\r\u009b"
	doc := map[string]any{
		"channels": map[string]any{evil: map[string]any{"type": "ntfy", evil: "v" + evil, "path": evil}, "odd": map[string]any{"type": evil}},
		"levels":   []any{map[string]any{"name": evil, "use_when": evil, "send": []any{map[string]any{"channel": evil, "interruption": evil}}}},
	}
	v := describe(doc, nil)
	text := strings.Join(v.lines(), "\n")
	b, _ := json.Marshal(v)
	expect(t, !hasControl(text) && len(v.lines()) == 7, "text view: %q (%d lines)", text, len(v.lines()))
	expect(t, !strings.ContainsAny(string(b), "\x1b\r\u009b") && strings.Count(text, "\n") == 6, "json view: %q", b)
}

// The usage and version paths follow the --json contract too.
func TestJSONWithoutACommandIsAUsageObject(t *testing.T) {
	r := newSandbox(t).do("", "--json")
	var obj struct {
		Version int
		Error   struct{ What, Valid, Fix string }
	}
	err := json.Unmarshal([]byte(r.out), &obj)
	expect(t, err == nil && r.code == ExitUsage && r.err == "" && obj.Version == 1 && obj.Error.What != "" && obj.Error.Fix != "", "code=%d out=%q err=%q (%v)", r.code, r.out, r.err, err)
}

func TestJSONVersion(t *testing.T) {
	r := newSandbox(t).do("", "version", "--json")
	var obj struct {
		Version int
		Status  string
		Release string
		Commit  string
	}
	err := json.Unmarshal([]byte(r.out), &obj)
	expect(t, err == nil && r.code == ExitOK && r.err == "", "code=%d out=%q err=%q (%v)", r.code, r.out, r.err, err)
	expect(t, obj.Version == 1 && obj.Status == "ok" && obj.Release == "dev" && obj.Commit == "none", "object = %+v", obj)
}

func TestJSONWarningsDropTheWarningPrefix(t *testing.T) {
	s := newSandbox(t)
	write(t, s.userConfig(), strings.Replace(userConfigBody, "{ channel: phone, interruption: time-sensitive }", "{ channel: phone, interruption: critical }", 1))
	r := s.do("", "config", "check", "--json")
	mustContain(t, "warnings", r.out, `"what":"level \"page\" sends critical`)
	mustNotContain(t, "warnings", r.out, `"what":"warning:`)
}

func TestEveryTextLineHasNoControlBytes(t *testing.T) {
	s := newSandbox(t)
	write(t, s.userConfig(), strings.Replace(userConfigBody, "FYI only", `FYI\e[2K\rforged`, 1))
	r := s.do("", "levels")
	expect(t, r.code == ExitOK && !hasControl(r.out), "levels output %q holds a control byte", r.out)
}
