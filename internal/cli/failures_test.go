package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

// everyCommand is one valid call of each command that works on the store.
func everyCommand() map[string][]string {
	return map[string][]string{
		"notify":       {"notify", "--level", "note", "--title", "t"},
		"resolve":      {"resolve", "--dedup-key", "k"},
		"flush":        {"flush"},
		"status":       {"status"},
		"channel test": {"channel", "test", "phone"},
	}
}

// breakDir makes a state subdirectory unreadable until the test ends.
func breakDir(t *testing.T, s *sandbox, sub string) {
	t.Helper()
	dir := filepath.Join(s.stateDir(), sub)
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
}

func TestCommandsFailOnAnUnusableStateDirectory(t *testing.T) {
	for name, args := range everyCommand() {
		s, _ := notifySandbox(t)
		t.Setenv("XDG_STATE_HOME", write(t, filepath.Join(s.home, "not-a-dir"), "x"))
		r := s.do("", args...)
		r.fails(t, name, ExitStore, "error: ")
	}
}

func TestCommandsFailOnAnUnreadableQueue(t *testing.T) {
	for name, args := range everyCommand() {
		s, _ := notifySandbox(t)
		s.notify("note") // creates the state directory
		breakDir(t, s, "queue")
		s.do("", args...).fails(t, name, ExitStore, "error: ")
	}
}

// The inflight directory is read when stranded sends are recovered, before any result exists.
func TestCommandsFailOnAnUnreadableInflightDirectory(t *testing.T) {
	for name, args := range everyCommand() {
		s, _ := notifySandbox(t)
		s.notify("note")
		breakDir(t, s, "inflight")
		r := s.do("", args...)
		switch name {
		case "channel test": // the test is its own outcome; the flush before it only warns
			expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT delivered ") && strings.Contains(r.err, "warning: "), "%s: %+v", name, r)
		default:
			r.fails(t, name, ExitStore, "error: ")
		}
	}
}

func TestStatusFailsOnAnUnreadableDeadLetter(t *testing.T) {
	s, _ := notifySandbox(t)
	s.notify("note")
	breakDir(t, s, "dead")
	s.do("", "status").fails(t, "status", ExitStore, "error: ")
}

func TestCommandsFailOnACorruptIncidentFile(t *testing.T) {
	s, _ := notifySandbox(t)
	s.notify("note", "--dedup-key", "k")
	write(t, filepath.Join(s.stateDir(), "incidents.json"), "{not json")
	s.do("", "status").fails(t, "status", ExitStore, "error: ")
	s.do("", "resolve", "--dedup-key", "k").fails(t, "resolve", ExitStore, "error: ")
}

func TestCommandsFailOnALooseSecretsFile(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["phone"], channel.Transient)
	s.notify("page", "--dedup-key", "k") // leaves a queued send and an incident that reached the pager
	s.advance(2 * time.Minute)
	if err := os.Chmod(s.secretsFile(), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, args := range everyCommand() {
		if name == "status" {
			s.do("", "status").succeeds(t, "status reads no secrets")
			continue
		}
		if name == "resolve" { // the incident is still closed and cancelled; the problem follows the result
			r := s.do("", args...)
			expect(t, r.code == ExitConfig && strings.HasPrefix(r.out, "RESULT resolved ") && strings.Contains(r.err, "chmod"), "resolve: %+v", r)
			continue
		}
		s.do("", args...).fails(t, name, ExitConfig, "chmod")
	}
}

func TestCommandsFailOnABrokenConfig(t *testing.T) {
	for name, args := range everyCommand() {
		s, _ := notifySandbox(t)
		write(t, s.userConfig(), "version: [")
		r := s.do("", args...)
		expect(t, r.code == ExitConfig && r.out == "" && strings.Contains(r.err, "error: "), "%s: %+v", name, r)
	}
}

// With nothing to build, no secret is read: a flush that has nothing due works on a bad secrets file.
func TestFlushWithNothingDueReadsNoSecrets(t *testing.T) {
	s, _ := notifySandbox(t)
	if err := os.Chmod(s.secretsFile(), 0o644); err != nil {
		t.Fatal(err)
	}
	r := s.do("", "flush")
	expect(t, r.code == ExitOK && firstLine(r.out) == "RESULT ok" && r.err == "", "flush: %+v", r)
}
