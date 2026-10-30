//go:build unix

package logch_test

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
)

func send(ch channel.Channel) error {
	_, err := ch.Send(context.Background(), channel.Message{ID: "01A"}, channel.SendSpec{Instance: "local", Interruption: channel.Passive})
	return err
}

// A log path that is a symlink is never followed: a trusted config must not turn a log line into
// an append to ~/.zshrc.
func TestLogRefusesASymlinkAndLeavesTheTargetAlone(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "startup")
	if err := os.WriteFile(target, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "yo.log")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	err := send(newLog(t, link))
	if err == nil || channel.KindOf(err) != channel.PermanentError {
		t.Errorf("err = %v (kind %v), want a permanent error", err, channel.KindOf(err))
	}
	if b, _ := os.ReadFile(target); string(b) != "original\n" {
		t.Errorf("the symlink target now holds %q", b)
	}
}

func TestLogRefusesWhatIsNotARegularFile(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"directory": dir, "fifo": fifo, "device": os.DevNull} {
		err := send(newLog(t, path))
		if err == nil || channel.KindOf(err) != channel.PermanentError {
			t.Errorf("%s: err = %v (kind %v), want a permanent error", name, err, channel.KindOf(err))
		}
	}
}
