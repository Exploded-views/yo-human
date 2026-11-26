package repolint

import (
	"reflect"
	"testing"
)

func TestNeverWakesSessionsFixtures(t *testing.T) {
	got, err := NeverWakesSessions("testdata/herdr")
	if err != nil {
		t.Fatalf("NeverWakesSessions: %v", err)
	}
	var paths []string
	for _, f := range got {
		paths = append(paths, f.Path)
	}
	want := []string{
		"cmd/yo-human-approved/bad.go", "cmd/yo-human-desk/bad.go",
		"internal/hosted/nudge/bad_test.go",
		"internal/hosted/web/bad_alias.go", "internal/hosted/web/bad_dial.go", "internal/hosted/web/bad_dialer.go",
		"internal/hosted/web/bad_dialtimeout.go", "internal/hosted/web/bad_dialunix.go", "internal/hosted/web/bad_dot.go",
		"internal/hosted/web/bad_dotaddr.go", "internal/hosted/web/bad_import.go", "internal/hosted/web/bad_literal.go",
		"internal/hosted/web/bad_unixaddr.go",
	}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
	for _, f := range got {
		if f.Msg == "" {
			t.Errorf("%s: no reason", f.Path)
		}
	}
}

// TestHostedNeverWakesSessions: the bus and its clients reach people by push, and reach sessions
// only by being read. No file of the hosted tree or its commands names herdr or dials a unix
// socket. A network passed as a variable or constant can't be checked statically: that case is
// best-effort, and review covers it.
func TestHostedNeverWakesSessions(t *testing.T) {
	got, err := NeverWakesSessions("../..")
	if err != nil {
		t.Fatalf("NeverWakesSessions: %v", err)
	}
	for _, f := range got {
		t.Errorf("%s: %s", f.Path, f.Msg)
	}
}
