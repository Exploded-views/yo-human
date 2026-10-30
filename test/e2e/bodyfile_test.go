//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The real binary never ships the secrets file as a body.
func TestBodyFileCannotCarryTheSecretsFile(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	link := filepath.Join(sb.cwd, "notes.txt")
	must(t, os.Symlink(sb.secretsPath(), link))
	for _, path := range []string{sb.secretsPath(), link} {
		r := sb.notify("note", "", "--body-file", path)
		r.wantExit(t, 64)
		if !strings.Contains(r.errOut, "user config directory") {
			t.Errorf("--body-file %s: stderr does not say why:\n%s", path, r.errOut)
		}
	}
	for _, f := range sb.fakesAll() {
		f.wantCount(t, 0)
	}
}

// A FIFO nobody writes to is refused at once; the call is killed if it hangs.
func TestBodyFileFIFODoesNotHang(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	fifo := filepath.Join(sb.cwd, "fifo")
	must(t, syscall.Mkfifo(fifo, 0o600))
	p := sb.start("notify", "--level", "note", "--title", "t", "--body-file", fifo)
	hung := time.AfterFunc(3*time.Second, func() { _ = p.cmd.Process.Kill() })
	r := p.wait()
	hung.Stop()
	r.wantExit(t, 64)
}

// A secret value an agent pastes into a message is replaced before it is queued or sent.
func TestASecretValueInAMessageIsRedacted(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	v := secretValues["pd-key"]
	r := sb.notify("note", "", "--body", "the routing key is "+v)
	r.want(t, 0, "delivered")
	for _, f := range sb.fakesAll() {
		for _, q := range f.requests() {
			if strings.Contains(string(q.Body), v) {
				t.Errorf("%s received the secret value", f.kind)
			}
		}
	}
	if !strings.Contains(r.errOut, "held the value of a secret") || strings.Contains(r.errOut, "pd-key") {
		t.Errorf("the warning must say a secret was redacted, never which:\n%s", r.errOut)
	}
}
