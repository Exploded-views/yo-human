// Package db is hosted mode's SQLite store. It implements bus.Store with database/sql and the
// pure-Go modernc.org/sqlite driver, and is the only place hosted state is written.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"

	_ "modernc.org/sqlite" // the driver, registered as "sqlite"
)

// ErrNotFound is returned when an update names a row that does not exist.
var ErrNotFound = errors.New("db: not found")

// timeLayout is fixed width, so stored times sort as strings. The zero time is stored as "".
const timeLayout = "2006-01-02T15:04:05.000000000Z"

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeLayout)
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(timeLayout, s)
}

// DB is the store. It is safe for concurrent use; writers are serialized by the database.
type DB struct {
	sql *sql.DB
}

var _ bus.Store = (*DB)(nil)

// Open opens (creating if need be) the database at path and migrates it to the latest schema.
// The parent directory must not be accessible to group or others: a missing one is created
// 0700, and one looser than 0700 is refused.
func Open(path string) (*DB, error) {
	if err := checkDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	// Create the file 0600 before SQLite does, so the WAL and shared-memory files follow.
	f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("db: create database file: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("db: create database file: %w", err)
	}
	raw, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	d := &DB{sql: raw}
	if err := migrate(context.Background(), raw); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return d, nil
}

// dsn gives every pooled connection WAL, a 5 s busy timeout, foreign keys, and BEGIN IMMEDIATE.
func dsn(path string) string {
	u := url.URL{Path: path}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Set("_txlock", "immediate")
	return "file:" + u.EscapedPath() + "?" + q.Encode()
}

func checkDir(dir string) error {
	fi, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("db: create data directory: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("db: data directory: %w", err)
	}
	if !fi.IsDir() {
		return errors.New("db: the database's parent is not a directory")
	}
	if fi.Mode().Perm()&^0o700 != 0 {
		return fmt.Errorf("db: data directory is %04o, looser than 0700: run chmod 700 on it", fi.Mode().Perm())
	}
	return nil
}

// Close closes the database.
func (d *DB) Close() error { return d.sql.Close() }

// Tx runs fn in one transaction, started with BEGIN IMMEDIATE. It commits if fn returns nil and
// rolls back on an error or a panic.
func (d *DB) Tx(ctx context.Context, fn func(bus.Tx) error) (err error) {
	t, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = t.Rollback()
			panic(p)
		}
	}()
	if err := fn(&txn{ctx: ctx, tx: t}); err != nil {
		_ = t.Rollback()
		return err
	}
	if err := t.Commit(); err != nil {
		return fmt.Errorf("db: commit: %w", err)
	}
	return nil
}

// txn is one transaction; it implements bus.Tx.
type txn struct {
	ctx context.Context
	tx  *sql.Tx
}

var _ bus.Tx = (*txn)(nil)

// Counts is what Check reports: SQLite's integrity verdict ("ok" when sound) and row counts.
type Counts struct {
	Integrity                          string
	Items, Decisions, Events, Messages int
}

// Check runs PRAGMA integrity_check and counts rows.
func (d *DB) Check(ctx context.Context) (Counts, error) {
	var c Counts
	var problems []string
	rows, err := d.sql.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return c, fmt.Errorf("db: integrity_check: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return c, fmt.Errorf("db: integrity_check: %w", err)
		}
		problems = append(problems, line)
	}
	if err := rows.Err(); err != nil {
		return c, fmt.Errorf("db: integrity_check: %w", err)
	}
	c.Integrity = "ok"
	if len(problems) != 1 || problems[0] != "ok" {
		c.Integrity = fmt.Sprintf("%d problems reported", len(problems))
	}
	for table, dst := range map[string]*int{"items": &c.Items, "decisions": &c.Decisions, "events": &c.Events, "messages": &c.Messages} {
		if err := d.sql.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(dst); err != nil { //nolint:gosec // table is a literal from the map above
			return c, fmt.Errorf("db: count %s: %w", table, err)
		}
	}
	return c, nil
}

// IsConflict reports whether err is a UNIQUE or PRIMARY KEY violation, the database's refusal
// of a second row where only one may exist.
func IsConflict(err error) bool {
	var se interface{ Code() int }
	if !errors.As(err, &se) {
		return false
	}
	const uniqueViolation, primaryKeyViolation = 2067, 1555
	c := se.Code()
	return c == uniqueViolation || c == primaryKeyViolation
}
