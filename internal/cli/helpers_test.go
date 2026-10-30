package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// sandbox is a throwaway HOME with no XDG overrides, no YO_HUMAN_CONFIG and a working directory
// of its own, so a run reads and writes nothing outside it.
type sandbox struct {
	t     *testing.T
	home  string
	clock *testClock
}

func newSandbox(t *testing.T) *sandbox {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("YO_HUMAN_CONFIG", "")
	t.Chdir(t.TempDir())
	s := &sandbox{t: t, home: home}
	old := openTTY
	t.Cleanup(func() { openTTY = old })
	s.tty("yes\n") // the human at their terminal confirms, unless a test says otherwise; /dev/tty is never opened
	return s
}

func (s *sandbox) configDir() string { return filepath.Join(s.home, ".config", "yo-human") }

// ownConfig is a fresh path for a --config file of the user's own: in their config directory, where
// it needs no trust.
func (s *sandbox) ownConfig(name string) string {
	return filepath.Join(s.configDir(), filepath.Base(s.t.TempDir()), name)
}
func (s *sandbox) userConfig() string { return filepath.Join(s.configDir(), "config.yaml") }
func (s *sandbox) secretsFile() string {
	return filepath.Join(s.configDir(), "secrets.yaml")
}

// run executes the CLI with stdin and returns the exit code, stdout and stderr.
func (s *sandbox) run(stdin string, args ...string) (int, string, string) {
	s.t.Helper()
	var out, errb bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

// write puts body at path (parents 0700, file 0600), the modes the secrets file demands.
func write(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// userConfigBody is a working ladder over an ntfy phone, a PagerDuty service and a log.
const userConfigBody = `version: 1
channels:
  phone: { type: ntfy, server: "https://ntfy.example.com", topic: { secret: ntfy-topic } }
  pager: { type: pagerduty, routing_key: { secret: pd-critical } }
  journal: { type: log, path: /tmp/yo-human-test.log }
levels:
  - name: note
    use_when: "FYI only"
    ask: optional
    send:
      - { channel: journal, interruption: passive }
      - { channel: phone, interruption: passive }
  - name: page
    use_when: "production is down"
    quiet_hours: send
    send:
      - { channel: phone, interruption: time-sensitive }
      - { channel: pager, interruption: critical }
`

func (s *sandbox) withConfig() { write(s.t, s.userConfig(), userConfigBody) }

func mustContain(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s = %q, want it to contain %q", what, got, w)
		}
	}
}

func mustNotContain(t *testing.T, what, got string, nots ...string) {
	t.Helper()
	for _, n := range nots {
		if strings.Contains(got, n) {
			t.Errorf("%s = %q, must not contain %q", what, got, n)
		}
	}
}

// firstLine is the first line of s.
func firstLine(s string) string { return strings.SplitN(s, "\n", 2)[0] }

func yamlUnmarshal(b []byte, v any) error { return yaml.Unmarshal(b, v) }

// expect reports a failed expectation; it keeps the branching out of the tests themselves.
func expect(t *testing.T, ok bool, format string, args ...any) {
	t.Helper()
	if !ok {
		t.Errorf(format, args...)
	}
}

// result is one run of the CLI.
type result struct {
	code     int
	out, err string
}

func (s *sandbox) do(stdin string, args ...string) result {
	s.t.Helper()
	code, out, errb := s.run(stdin, args...)
	return result{code, out, errb}
}

// fails expects exit code, nothing on stdout, and msg on stderr.
func (r result) fails(t *testing.T, label string, code int, msg string) {
	t.Helper()
	expect(t, r.code == code && r.out == "" && strings.Contains(r.err, msg), "%s: code=%d out=%q err=%q; want exit %d, no stdout and %q on stderr", label, r.code, r.out, r.err, code, msg)
}

// succeeds expects exit 0 with RESULT ok first.
func (r result) succeeds(t *testing.T, label string) {
	t.Helper()
	expect(t, r.code == ExitOK && firstLine(r.out) == "RESULT ok", "%s: code=%d out=%q err=%q; want exit 0 and RESULT ok", label, r.code, r.out, r.err)
}
