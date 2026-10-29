package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The edges of the security fixes: each failure path says what went wrong and changes nothing.

func TestBodyFileThatCannotBeOpenedOrReadIsAUsageProblem(t *testing.T) {
	s, fk := notifySandbox(t)
	locked := write(t, filepath.Join(t.TempDir(), "locked.txt"), "x")
	expect(t, os.Chmod(locked, 0) == nil, "chmod")
	s.notify("page", "--body-file", locked).fails(t, "unreadable body file", ExitUsage, "cannot be opened")
	var out, errb strings.Builder
	code := run([]string{"notify", "--level", "page", "--title", "t", "--ask", "a", "--body", "-"}, brokenReader{}, &out, &errb)
	expect(t, code == ExitUsage && strings.Contains(errb.String(), "cannot be read"), "a body on a broken stdin: code=%d err=%q", code, errb.String())
	expect(t, len(fk["pager"].Sent) == 0, "a message went out")
}

// Without a working directory the path cannot be resolved, and the identity walk still finds a
// file of the user config directory.
func TestBodyFileIsRefusedEvenWhenItsPathCannotBeResolved(t *testing.T) {
	s, _ := notifySandbox(t)
	deep := write(t, filepath.Join(s.configDir(), "sub", "notes.txt"), "FAKE-DEEP-NOTE")
	old := absPath
	absPath = func(string) (string, error) { return "", errors.New("getwd: no such file or directory") }
	t.Cleanup(func() { absPath = old })
	s.notify("page", "--body-file", deep).fails(t, "a file in the user config directory with no working directory", ExitUsage, "user config directory")
}

func TestPageRefusesAnOverlongProject(t *testing.T) {
	s, fk := notifySandbox(t)
	write(t, s.userConfig(), notifyConfigBody+"shim:\n  default: { level: page, ask: \"look\" }\n  low: { level: note, ask: \"later\" }\n  agent: orchestrator\n")
	t.Setenv("YO_HUMAN_PROJECT", strings.Repeat("p", 65))
	s.do("", "page", "db down", "", "k").fails(t, "page with a long project", ExitUsage, "64 characters")
	s.do("", "page", "--resolve", "k").fails(t, "page --resolve with a long project", ExitUsage, "64 characters")
	expect(t, len(fk["pager"].Sent) == 0, "a page went out")
}

// A resolve note is redacted against the secrets file, so a secrets file yo-human refuses to read
// stops the resolve before anything is sent.
func TestResolveNoteWithAnUnreadableSecretsFileIsAConfigProblem(t *testing.T) {
	s, _ := notifySandbox(t)
	expect(t, os.Chmod(s.secretsFile(), 0o644) == nil, "chmod")
	r := s.do("", "resolve", "--dedup-key", "k", "--note", "fixed")
	expect(t, r.code == ExitConfig, "resolve with a loose secrets file: %+v; want 78", r)
}

// status hides the queued and held entries of another project, as it hides its incidents.
func TestStatusHidesAnotherProjectsEntries(t *testing.T) {
	s, _ := notifySandbox(t)
	s.notify("night", "--project", "beta") // held for the quiet hours
	t.Setenv("YO_HUMAN_PROJECT", "alpha")
	r := s.do("", "status")
	mustContain(t, "status as alpha", r.out, "held: 0")
	t.Setenv("YO_HUMAN_PROJECT", "")
	r = s.do("", "status", "--all") // the human, with no project set
	mustContain(t, "status --all", r.out, "held: 1")
}

// The trust display renders whatever YAML can hold: non-string keys, dates, a NaN, a tab, and a
// format character outside the basic plane.
func TestTrustDisplayRendersEveryKindOfValue(t *testing.T) {
	out := described(t, "version: 1\nhuman: { name: \"a\\tb\\U000E0001c\" }\nlevels:\n  - name: page\n    use_when: x\n"+
		"    send: [ { channel: a, interruption: passive, options: { odd: { 1: one }, when: 2026-10-26, nan: .nan } } ]\n")
	mustContain(t, "trust display", out, `"a\tb\U000e0001c"`, `"odd":{"1":"one"}`, `"when":"2026-10-26T00:00:00Z"`, `"nan":<a value that cannot be shown>`)
}

// The real openTTY opens the controlling terminal, or fails where there is none; either way it
// returns at once.
func TestTheRealTerminalOpens(t *testing.T) {
	if tty, err := realTTY(); err == nil {
		_ = tty.Close()
	}
}

// A noninteractive run whose audit line cannot be written changes nothing.
func TestNoninteractiveRunThatCannotBeAuditedChangesNothing(t *testing.T) {
	s := newSandbox(t)
	s.noTTY()
	t.Setenv("YO_HUMAN_NONINTERACTIVE", "1")
	t.Setenv("XDG_STATE_HOME", write(t, filepath.Join(s.home, "state-blocker"), "file"))
	s.do("FAKE-VALUE\n", "secret", "set", "pd-critical").fails(t, "secret set with no audit log", ExitStore, "error: ")
	_, err := os.Stat(s.secretsFile())
	expect(t, errors.Is(err, os.ErrNotExist), "the secret was stored without its audit line: %v", err)
}

// realTTY is openTTY as the binary has it, before any test replaces it.
var realTTY = openTTY
