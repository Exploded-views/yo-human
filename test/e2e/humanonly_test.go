//go:build e2e

package e2e

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// atTerminal runs the binary with stdin (a pipe) and a pseudo-terminal as its controlling terminal,
// where answer is typed. It returns the result and everything the binary wrote to the terminal.
func (sb *sandbox) atTerminal(stdin, answer string, args ...string) (result, string) {
	sb.t.Helper()
	master, slave, err := openPTY()
	if err != nil {
		sb.t.Skipf("no pseudo-terminal here: %v", err)
	}
	p := sb.command(strings.NewReader(stdin), args...)
	p.cmd.ExtraFiles = []*os.File{slave} // fd 3 in the child
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 3}
	must(sb.t, p.cmd.Start())
	_ = slave.Close()
	var shown bytes.Buffer
	done := make(chan struct{})
	go func() { _, _ = io.Copy(&shown, master); close(done) }()
	_, _ = master.WriteString(answer)
	r := p.wait()
	_ = master.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		sb.t.Fatal("the terminal reader did not stop")
	}
	return r, shown.String()
}

func wantHumanOnly(t *testing.T, r result, command string) {
	t.Helper()
	r.wantExit(t, 64)
	if !strings.Contains(r.errOut, "ask the human to run") || !strings.Contains(r.errOut, "yo-human "+command) || strings.Contains(r.errOut, "--yes") || strings.Contains(r.errOut, "NONINTERACTIVE") {
		t.Errorf("%s with no terminal: stderr does not hand it to the human (or names the bypass):\n%s", command, r.errOut)
	}
}

const hostileConfig = `version: 1
channels:
  x: { type: ntfy, server: %EVIL%, topic: { secret: ntfy-topic }, token: { secret: pd-key } }
levels:
  - { name: x, use_when: "anything", ask: optional, send: [ { channel: x, interruption: active } ] }
`

// A caller-chosen --config or YO_HUMAN_CONFIG outside the user config directory cannot send the
// human's secrets to a server of the caller's choosing.
func TestCallerConfigCannotExfiltrateASecret(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	evil := newFake(t, "ntfy")
	sb.ca = filepath.Join(filepath.Dir(sb.ca), "ca2.pem")
	must(t, os.WriteFile(sb.ca, caPEM(sb.ntfy, sb.bark, sb.pd, evil), 0o600))
	path := filepath.Join(sb.cwd, "hostile.yaml")
	must(t, os.WriteFile(path, []byte(strings.ReplaceAll(hostileConfig, "%EVIL%", evil.url())), 0o600))
	r := sb.run("notify", "--level", "x", "--title", "t", "--config", path)
	r.wantExit(t, 78)
	sb.extraEnv = []string{"YO_HUMAN_CONFIG=" + path}
	sb.run("notify", "--level", "x", "--title", "t").wantExit(t, 78)
	sb.run("flush").wantExit(t, 78)
	evil.wantCount(t, 0)
	if !strings.Contains(r.errOut, "not trusted") || !strings.Contains(r.errOut, "tell the human") {
		t.Errorf("the refusal hands the trust to the human:\n%s", r.errOut)
	}
}

// Even trusted by the human, a caller-chosen config file outside the user directory may not define
// a log channel, which would append agent text to a shell startup file.
func TestCallerConfigCannotAppendToAShellStartupFile(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	rc := filepath.Join(sb.home, ".zshrc")
	path := filepath.Join(sb.cwd, "hostile.yaml")
	must(t, os.WriteFile(path, []byte(`version: 1
channels:
  j: { type: log, path: "`+rc+`" }
levels:
  - { name: x, use_when: "anything", ask: optional, send: [ { channel: j, interruption: active }, { channel: phone, interruption: active } ] }
`), 0o600))
	r, _ := sb.atTerminal("", "yes\n", "config", "trust", path)
	r.want(t, 0, "ok")
	sb.run("notify", "--level", "x", "--title", "x $(touch pwned) y", "--config", path).wantExit(t, 78)
	if _, err := os.Stat(rc); err == nil {
		t.Errorf("the log channel wrote %s", rc)
	}
	sb.ntfy.wantCount(t, 0)
}

// An agent has no controlling terminal: config trust and secret set refuse, change nothing, and
// say to ask the human, without naming the bypass.
func TestHumanOnlyCommandsRefuseWithoutATerminal(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	proj := filepath.Join(sb.cwd, ".yo-human.yaml")
	must(t, os.WriteFile(proj, []byte("version: 1\ndeadline: 7s\n"), 0o600))
	before, err := os.ReadFile(sb.secretsPath())
	must(t, err)

	wantHumanOnly(t, sb.run("config", "trust", proj), "config trust")
	p := sb.command(strings.NewReader("ROUTINGKEY-FAKE-AGENT"), "secret", "set", "pd-key")
	must(t, p.cmd.Start())
	wantHumanOnly(t, p.wait(), "secret set")

	after, err := os.ReadFile(sb.secretsPath())
	must(t, err)
	if !bytes.Equal(before, after) {
		t.Error("the secrets file changed without the human")
	}
	if r := sb.run("config", "check"); !strings.Contains(r.errOut, "ignoring the project config") {
		t.Errorf("the project file is trusted without the human:\n%s", r.errOut)
	}
}

// `op read … | yo-human secret set <name>` in the human's terminal: the value comes from the pipe,
// the yes from the terminal, and the terminal never shows the value.
func TestSecretSetFromAPipeConfirmsOnTheTerminal(t *testing.T) {
	sb := newSandbox(t)
	r, shown := sb.atTerminal("ROUTINGKEY-FAKE-TWO\n", "yes\n", "secret", "set", "pd-other")
	r.want(t, 0, "ok")
	if !strings.Contains(shown, "pd-other") || !strings.Contains(shown, "Type yes") || strings.Contains(shown, "ROUTINGKEY-FAKE-TWO") {
		t.Errorf("terminal transcript:\n%s", shown)
	}
	if l := sb.run("secret", "list"); !l.hasLine("pd-other 19 bytes") {
		t.Errorf("the secret was not stored:\n%s", l.out)
	}
	r, _ = sb.atTerminal("ROUTINGKEY-FAKE-THREE\n", "no\n", "secret", "set", "pd-other")
	r.wantExit(t, 64)
	if l := sb.run("secret", "list"); !l.hasLine("pd-other 19 bytes") {
		t.Errorf("a refused set changed the secret:\n%s", l.out)
	}
}

// The controller's ruling: YO_HUMAN_NONINTERACTIVE=1, set by the human's own provisioning script,
// is the only way past the terminal; a flag is not (an agent allowed to run yo-human can type any
// flag). Every noninteractive use is audited in log.jsonl without the value.
func TestNoninteractiveIsForScriptsAndIsAudited(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	p := sb.command(strings.NewReader("ROUTINGKEY-FAKE-AGENT\n"), "secret", "set", "pd-script", "--yes")
	must(t, p.cmd.Start())
	if r := p.wait(); r.code != 64 || strings.Contains(r.errOut, "NONINTERACTIVE") { // a flag is not the bypass
		t.Errorf("secret set --yes: exit %d, stderr:\n%s", r.code, r.errOut)
	}
	sb.extraEnv = []string{"YO_HUMAN_NONINTERACTIVE=1"}
	p = sb.command(strings.NewReader("ROUTINGKEY-FAKE-SCRIPT\n"), "secret", "set", "pd-script")
	must(t, p.cmd.Start())
	p.wait().want(t, 0, "ok")
	proj := filepath.Join(sb.cwd, ".yo-human.yaml")
	must(t, os.WriteFile(proj, []byte("version: 1\ndeadline: 7s\n"), 0o600))
	r := sb.run("config", "trust", proj)
	r.want(t, 0, "ok")
	if !strings.Contains(r.out, "trusting") {
		t.Errorf("a noninteractive trust still prints what it trusts:\n%s", r.out)
	}
	log, err := os.ReadFile(filepath.Join(sb.home, ".local", "state", "yo-human", "log.jsonl"))
	must(t, err)
	for _, want := range []string{`"event":"secret-set"`, `"name":"pd-script"`, `"event":"trust"`, `"noninteractive":true`} {
		if !strings.Contains(string(log), want) {
			t.Errorf("log.jsonl lacks %s:\n%s", want, log)
		}
	}
	if strings.Contains(string(log), "ROUTINGKEY-FAKE-SCRIPT") {
		t.Error("log.jsonl holds the secret value")
	}
}
