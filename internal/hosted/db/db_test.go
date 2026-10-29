package db_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"

	_ "modernc.org/sqlite"
)

// userVersion reads PRAGMA user_version through a separate connection.
func userVersion(t *testing.T, path string) int {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	var v int
	if err := raw.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestOpenMigratesAndReopens(t *testing.T) {
	d, path := openTemp(t)
	insertItem(t, d, item("itm_1", "crew", "first"))
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if v := userVersion(t, path); v != 2 {
		t.Fatalf("user_version = %d, want 2", v)
	}
	d2, err := db.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = d2.Close() }()
	if v := userVersion(t, path); v != 2 {
		t.Fatalf("user_version after the second migration run = %d, want 2", v)
	}
	var found bool
	tx(t, d2, func(x bus.Tx) (err error) { _, found, err = x.ItemByID("itm_1"); return })
	if !found {
		t.Fatal("the item written before the reopen is gone: the second run was not a no-op")
	}
}

func TestOpenRefusesLooseDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "loose")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := db.Open(filepath.Join(dir, "bus.db"))
	if err == nil {
		t.Fatal("Open accepted a 0755 parent directory")
	}
	if !strings.Contains(err.Error(), "chmod 700") {
		t.Fatalf("error %q does not name chmod 700", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "bus.db")); statErr == nil {
		t.Fatal("a database file was created in the loose directory")
	}
}

func TestOpenCreatesMissingParentPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new")
	d, err := db.Open(filepath.Join(dir, "bus.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	fi, err := os.Stat(dir)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("parent mode = %v, %v; want 0700", fi, err)
	}
}

func TestOpenRefusesNonDirectoryParent(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Open(filepath.Join(file, "bus.db")); err == nil {
		t.Fatal("Open accepted a parent that is a file")
	}
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	d, path := openTemp(t)
	_ = d.Close()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()
	if _, err := db.Open(path); err == nil {
		t.Fatal("Open accepted a database newer than this binary")
	}
}

func TestTxRollsBackOnError(t *testing.T) {
	d, _ := openTemp(t)
	boom := errors.New("boom")
	err := d.Tx(context.Background(), func(x bus.Tx) error {
		if err := x.InsertItem(item("itm_1", "crew", "first")); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Tx returned %v, want boom", err)
	}
	var found bool
	tx(t, d, func(x bus.Tx) (err error) { _, found, err = x.ItemByID("itm_1"); return })
	if found {
		t.Fatal("a row survived the failed transaction")
	}
}

func TestTxRollsBackOnPanic(t *testing.T) {
	d, _ := openTemp(t)
	func() {
		defer func() { _ = recover() }()
		_ = d.Tx(context.Background(), func(x bus.Tx) error {
			_ = x.InsertItem(item("itm_1", "crew", "first"))
			panic("boom")
		})
	}()
	var found bool
	tx(t, d, func(x bus.Tx) (err error) { _, found, err = x.ItemByID("itm_1"); return })
	if found {
		t.Fatal("a row survived the panicking transaction")
	}
}

func TestTxHonoursCancelledContext(t *testing.T) {
	d, _ := openTemp(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Tx(ctx, func(bus.Tx) error { return nil }); err == nil {
		t.Fatal("Tx ran on a cancelled context")
	}
}

func TestCheckReportsIntegrity(t *testing.T) {
	d, _ := openTemp(t)
	c, err := d.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.Integrity != "ok" {
		t.Fatalf("integrity = %q, want ok", c.Integrity)
	}
	if c.Items != 0 || c.Decisions != 0 || c.Relays != 0 || c.Events != 0 || c.Messages != 0 {
		t.Fatalf("fresh database has counts %+v", c)
	}
	insertItem(t, d, item("itm_1", "crew", "first"))
	if c, err = d.Check(context.Background()); err != nil || c.Items != 1 {
		t.Fatalf("after one item: %+v, %v", c, err)
	}
}

func TestOpenRefusesLooseFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "bus.db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := db.Open(path)
	if err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("Open of a 0644 file gave %v, want a chmod 600 refusal", err)
	}
}

func TestOpenSetsFileModeAndWAL(t *testing.T) {
	d, path := openTemp(t)
	insertItem(t, d, item("itm_1", "crew", "first"))
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("database file mode = %v, %v; want 0600", fi, err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	var mode string
	if err := raw.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v; want wal", mode, err)
	}
}
