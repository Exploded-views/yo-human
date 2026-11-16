package db_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

// rawExec runs SQL on the database file behind the store's back.
func rawExec(t *testing.T, path string, stmts ...string) {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	for _, s := range stmts {
		if _, err := raw.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func TestTokenStoreReportsQueryFailures(t *testing.T) {
	d, path := openTemp(t)
	must(t, d.InsertNode("ci-1", "crew", "yhn_abcd", "h1", t0))
	rawExec(t, path, "DROP TABLE tokens", "DROP TABLE desk_tokens")
	calls := map[string]func() error{
		"NodeByHash":    func() error { _, _, err := d.NodeByHash("h1"); return err },
		"StarterByHash": func() error { _, _, err := d.StarterByHash("h1"); return err },
		"DeskByHash":    func() error { _, _, err := d.DeskByHash("h1"); return err },
		"TouchNode":     func() error { return d.TouchNode("ci-1", t0) },
		"InsertNode":    func() error { return d.InsertNode("ci-2", "crew", "yhn_abcd", "h2", t0) },
		"InsertStarter": func() error { return d.InsertStarter("mac", "yhd_abcd", "h3", t0, 2) },
		"InsertDesk":    func() error { return d.InsertDesk("h4", t0) },
		"ListTokens":    func() error { _, err := d.ListTokens(); return err },
		"Revoke":        func() error { _, err := d.Revoke("ci-1", t0); return err },
		"RevokeDesk":    func() error { _, _, err := d.RevokeDesk(t0); return err },
	}
	for name, call := range calls {
		err := call()
		if err == nil || errors.Is(err, db.ErrStarterCap) || db.IsConflict(err) {
			t.Fatalf("%s on a missing table: err=%v, want a plain store error", name, err)
		}
	}
}

func TestTokenStoreReportsUnreadableTimes(t *testing.T) {
	d, path := openTemp(t)
	rawExec(t, path,
		"INSERT INTO tokens (name, kind, swarm, shown, hash, created) VALUES ('n', 'node', 'crew', 'yhn_abcd', 'bad-n', 'not a time')",
		"INSERT INTO tokens (name, kind, swarm, shown, hash, created) VALUES ('s', 'desk-starter', '', 'yhd_abcd', 'bad-s', 'not a time')",
		"INSERT INTO desk_tokens (hash, created) VALUES ('bad-d', 'not a time')",
	)
	calls := map[string]func() error{
		"NodeByHash":    func() error { _, _, err := d.NodeByHash("bad-n"); return err },
		"StarterByHash": func() error { _, _, err := d.StarterByHash("bad-s"); return err },
		"DeskByHash":    func() error { _, _, err := d.DeskByHash("bad-d"); return err },
		"ListTokens":    func() error { _, err := d.ListTokens(); return err },
	}
	for name, call := range calls {
		if err := call(); err == nil {
			t.Fatalf("%s read a row whose time is unreadable", name)
		}
	}
}

func TestStarterCapCountFailureStoresNothing(t *testing.T) {
	d, path := openTemp(t)
	// A trigger that refuses the insert after the count passed: the cap's own transaction rolls back.
	rawExec(t, path, "CREATE TRIGGER no_starters BEFORE INSERT ON tokens WHEN NEW.kind = 'desk-starter' BEGIN SELECT RAISE(ABORT, 'refused'); END")
	if err := d.InsertStarter("mac", "yhd_abcd", "h1", t0, 2); err == nil || errors.Is(err, db.ErrStarterCap) {
		t.Fatalf("err=%v, want the insert's own failure", err)
	}
}
