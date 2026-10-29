//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The secret values. Each matches ^[A-Z0-9-]*FAKE[A-Z0-9-]*$; none may appear in any output.
var secretValues = map[string]string{
	"ntfy-topic": "TOPIC-FAKE-ONE",
	"ntfy-token": "TOK-FAKE-1",
	"bark-key":   "BARKKEY-FAKE-ONE",
	"pd-key":     "ROUTINGKEY-FAKE-ONE",
}

// sandbox is one scenario's world: a temp HOME, a working directory, three fakes and a clock.
type sandbox struct {
	t              *testing.T
	home, cwd, ca  string
	ntfy, bark, pd *fake
	at             time.Time // the clock the binary sees (YO_HUMAN_TEST_NOW)
	extraEnv       []string  // more variables for the binary, such as YO_HUMAN_CONFIG
}

func newSandbox(t *testing.T) *sandbox {
	t.Helper()
	root := t.TempDir()
	sb := &sandbox{t: t, home: filepath.Join(root, "home"), cwd: filepath.Join(root, "work"),
		ntfy: newFake(t, "ntfy"), bark: newFake(t, "bark"), pd: newFake(t, "pagerduty"),
		at: time.Now().UTC().Truncate(time.Second)}
	for _, d := range []string{sb.home, sb.cwd} {
		must(t, os.MkdirAll(d, 0o700))
	}
	sb.ca = filepath.Join(root, "ca.pem")
	must(t, os.WriteFile(sb.ca, caPEM(sb.ntfy, sb.bark, sb.pd), 0o600))
	sb.writeSecrets(0o600)
	return sb
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (sb *sandbox) configDir() string { return filepath.Join(sb.home, ".config", "yo-human") }

func (sb *sandbox) secretsPath() string { return filepath.Join(sb.configDir(), "secrets.yaml") }

// writeSecrets writes the secrets file with the given mode, in a 0700 directory.
func (sb *sandbox) writeSecrets(mode os.FileMode) {
	sb.t.Helper()
	must(sb.t, os.MkdirAll(sb.configDir(), 0o700))
	var b strings.Builder
	for name, v := range secretValues {
		b.WriteString(name + ": " + v + "\n")
	}
	must(sb.t, os.WriteFile(sb.secretsPath(), []byte(b.String()), mode))
	must(sb.t, os.Chmod(sb.secretsPath(), mode))
}

// writeConfig writes the user config, filling the fakes' addresses in.
func (sb *sandbox) writeConfig(text string) {
	sb.t.Helper()
	text = strings.NewReplacer("%NTFY%", sb.ntfy.url(), "%BARK%", sb.bark.url(), "%PD%", sb.pd.url()).Replace(text)
	must(sb.t, os.MkdirAll(sb.configDir(), 0o700))
	must(sb.t, os.WriteFile(filepath.Join(sb.configDir(), "config.yaml"), []byte(text), 0o600))
}

// advance moves the binary's clock forward.
func (sb *sandbox) advance(d time.Duration) { sb.at = sb.at.Add(d) }

// env is the whole environment the binary gets: nothing is inherited. The proxy variables point at
// a closed port, so any call that leaves the fakes (to ntfy.sh, api.day.app or
// events.pagerduty.com) fails loudly; loopback is never proxied, so the fakes still answer.
func (sb *sandbox) env() []string {
	return []string{
		"HOME=" + sb.home,
		"XDG_CONFIG_HOME=" + filepath.Join(sb.home, ".config"),
		"XDG_STATE_HOME=" + filepath.Join(sb.home, ".local", "state"),
		"PATH=/usr/bin:/bin",
		"YO_HUMAN_PROJECT=proj", // the project notify names: resolve and status see its incidents
		"YO_HUMAN_TEST_NOW=" + sb.at.Format(time.RFC3339),
		"YO_HUMAN_TEST_CA=" + sb.ca,
		"HTTPS_PROXY=http://127.0.0.1:1", "HTTP_PROXY=http://127.0.0.1:1",
		"https_proxy=http://127.0.0.1:1", "http_proxy=http://127.0.0.1:1",
		"NO_PROXY=127.0.0.1", "no_proxy=127.0.0.1",
	}
}

// result is one finished run.
type result struct {
	code        int
	out, errOut string
}

// proc is a run in progress.
type proc struct {
	sb       *sandbox
	cmd      *exec.Cmd
	out, err bytes.Buffer
}

func (sb *sandbox) start(args ...string) *proc {
	sb.t.Helper()
	p := sb.command(nil, args...)
	must(sb.t, p.cmd.Start())
	return p
}

// command prepares a run with stdin. It runs in a session of its own with no controlling terminal,
// as an agent's commands do, so a run never reaches the terminal of whoever runs the tests.
func (sb *sandbox) command(stdin io.Reader, args ...string) *proc {
	p := &proc{sb: sb, cmd: exec.Command(binPath, args...)}
	p.cmd.Dir, p.cmd.Env = sb.cwd, append(sb.env(), sb.extraEnv...)
	p.cmd.Stdin, p.cmd.Stdout, p.cmd.Stderr = stdin, &p.out, &p.err
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return p
}

func (p *proc) wait() result {
	p.sb.t.Helper()
	err := p.cmd.Wait()
	code := 0
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		code = ee.ExitCode()
	case err != nil:
		p.sb.t.Fatal(err)
	}
	r := result{code, p.out.String(), p.err.String()}
	for name, v := range secretValues {
		if strings.Contains(r.out, v) || strings.Contains(r.errOut, v) {
			p.sb.t.Fatalf("the value of secret %s appears in the output of %v", name, p.cmd.Args[1:])
		}
	}
	p.sb.t.Logf("yo-human %s -> exit %d\n%s%s", strings.Join(p.cmd.Args[1:], " "), code, r.out, r.errOut)
	return r
}

// run runs the binary to completion.
func (sb *sandbox) run(args ...string) result { sb.t.Helper(); return sb.start(args...).wait() }

func (r result) wantExit(t *testing.T, want int) {
	t.Helper()
	if r.code != want {
		t.Fatalf("exit code %d, want %d\nstdout:\n%sstderr:\n%s", r.code, want, r.out, r.errOut)
	}
}

var resultLine = regexp.MustCompile(`^RESULT (\w+)(?: (\S+))?$`)

// head is the status and id of the first stdout line, which must be a RESULT line.
func (r result) head(t *testing.T) (status, id string) {
	t.Helper()
	first, _, _ := strings.Cut(r.out, "\n")
	m := resultLine.FindStringSubmatch(first)
	if m == nil {
		t.Fatalf("first stdout line %q is not a RESULT line", first)
	}
	return m[1], m[2]
}

// want checks the exit code and the RESULT status, and returns the id.
func (r result) want(t *testing.T, code int, status string) string {
	t.Helper()
	r.wantExit(t, code)
	got, id := r.head(t)
	if got != status {
		t.Fatalf("RESULT %s, want %s\nstdout:\n%s", got, status, r.out)
	}
	return id
}

// hasLine reports whether a stdout line is exactly want.
func (r result) hasLine(want string) bool {
	for _, l := range strings.Split(r.out, "\n") {
		if l == want {
			return true
		}
	}
	return false
}

// view is the part of `status --json` the scenarios read.
type view struct {
	Status string `json:"status"`
	Queued []struct {
		ID, Channel, Kind string
		Attempts          int
	} `json:"queued"`
	Held []struct {
		ID, Channel, Kind string
		DeliverAt         time.Time `json:"deliver_at"`
	} `json:"held"`
	Dead []struct {
		ID, Channel, Kind, Reason string
	} `json:"dead"`
	Incidents []struct {
		Key, Level string
		Generation int
	} `json:"incidents"`
}

// status runs `status --json` as project proj, the project every scenario notifies as. Like every
// command it first flushes what is due at the clock.
func (sb *sandbox) status() view {
	sb.t.Helper()
	r := sb.run("status", "--json")
	r.wantExit(sb.t, 0)
	var v view
	must(sb.t, json.Unmarshal([]byte(r.out), &v))
	return v
}

// notify runs notify at a level with an ask, a title and a dedup key.
func (sb *sandbox) notify(level, key string, extra ...string) result {
	sb.t.Helper()
	args := []string{"notify", "--level", level, "--title", "Disk is full on build-7", "--ask", "Free some space",
		"--project", "proj", "--agent", "tester"}
	if key != "" {
		args = append(args, "--dedup-key", key)
	}
	return sb.run(append(args, extra...)...)
}

// expect fails the test for each key of want whose value in got differs. JSON numbers are float64.
func expect(t *testing.T, label string, got map[string]any, want map[string]any) {
	t.Helper()
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: %s is %v, want %v", label, k, got[k], w)
		}
	}
}

// sub is the nested JSON object under key.
func sub(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}

// fakesAll are the three provider fakes.
func (sb *sandbox) fakesAll() []*fake { return []*fake{sb.ntfy, sb.bark, sb.pd} }

// wantLines fails unless every line is among the stdout lines.
func wantLines(t *testing.T, r result, lines ...string) {
	t.Helper()
	for _, l := range lines {
		if !r.hasLine(l) {
			t.Errorf("stdout lacks the line %q:\n%s", l, r.out)
		}
	}
}
