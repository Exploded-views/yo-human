package app

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// breakTable drops a table behind the app's back, so the command opens the database and then
// fails on its query.
func (a adminEnv) breakTable(table string) {
	a.t.Helper()
	raw, err := sql.Open("sqlite", a.path)
	if err != nil {
		a.t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	if _, err := raw.Exec("DROP TABLE " + table); err != nil {
		a.t.Fatal(err)
	}
}

func TestAdminQueryFailureExitsStore(t *testing.T) {
	for _, c := range []struct {
		table string
		args  []string
	}{
		{"tokens", []string{"token", "list"}},
		{"tokens", []string{"check"}},
		{"tokens", []string{"token", "revoke", "--name", "ci-1"}},
		{"desk_tokens", []string{"token", "revoke", "--desk-all"}},
	} {
		a := newAdmin(t)
		a.create("token", "create", "--name", "ci-1", "--swarm", "crew")
		a.breakTable(c.table)
		code, out, errs := a.run(c.args...)
		if code != ExitStore || out != "" || !strings.Contains(errs, "no such table") {
			t.Fatalf("%v: code=%d out=%q err=%q", c.args, code, out, errs)
		}
	}
}

func TestAdminBadFlagsAreUsage(t *testing.T) {
	for _, args := range [][]string{
		{"token", "create-desk-starter", "--bogus"},
		{"token", "list", "--bogus"},
		{"token", "revoke", "--bogus"},
		{"token", "revoke", "-h"},
	} {
		a := newAdmin(t)
		code, out, _ := a.run(args...)
		want := ExitUsage
		if args[len(args)-1] == "-h" {
			want = ExitOK
		}
		if code != want || out != "" {
			t.Fatalf("%v: code=%d out=%q", args, code, out)
		}
	}
}

func TestConfigRefusesBadApproveListen(t *testing.T) {
	for _, bad := range []string{"host:8080", ":abc", ":0", ":70000", "[::1:8080", ":"} {
		if _, err := LoadConfig(envOf(with("YH_LISTEN_APPROVE", bad))); err == nil || !strings.Contains(err.Error(), "YH_LISTEN_APPROVE") {
			t.Fatalf("YH_LISTEN_APPROVE=%q: err=%v", bad, err)
		}
	}
	for _, ok := range []string{":9090", "0.0.0.0:9090", "[::]:9090", "127.0.0.1:9090"} {
		if _, err := LoadConfig(envOf(with("YH_LISTEN_APPROVE", ok))); err != nil {
			t.Fatalf("YH_LISTEN_APPROVE=%q: %v", ok, err)
		}
	}
}
