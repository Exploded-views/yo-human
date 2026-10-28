package db_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

var t0 = time.Date(2026, 10, 28, 9, 30, 15, 123456000, time.UTC)

// openTemp opens a database in a fresh 0700 temp directory.
func openTemp(t *testing.T) (*db.DB, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "bus.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d, path
}

func tx(t *testing.T, d *db.DB, fn func(bus.Tx) error) {
	t.Helper()
	if err := d.Tx(context.Background(), fn); err != nil {
		t.Fatalf("Tx: %v", err)
	}
}

// item is a fully populated item in swarm with the given id and handle.
func item(id, swarm, handle string) bus.Item {
	return bus.Item{
		Draft: bus.Draft{
			Kind: bus.KindApproval, Severity: bus.SeverityHigh, To: "human", Handle: handle,
			Title: "Merge the change", Summary: "A summary.", Detail: "Some\ndetail.",
			RequiredAction: "Say yes.", Repo: "owner/repo", Ref: "42", BaseRef: "main",
			Target: "repo", PinnedCommit: "abc123", PinnedTree: "def456",
			Action: bus.ActionMerge, Link: "https://example.org/pr/42",
			RiskFlags: []string{"touches-auth", "large-diff"}, Untrusted: true,
		},
		ID: id, Swarm: swarm, From: "ci-1", FromKind: bus.PrincipalNode, Revision: 1,
		State: bus.StateOpen, ContentHash: "hash-1", LastSeq: 7, AckedSeq: 3,
		Created: t0, Updated: t0.Add(time.Minute),
	}
}

func insertItem(t *testing.T, d *db.DB, it bus.Item) {
	t.Helper()
	tx(t, d, func(x bus.Tx) error { return x.InsertItem(it) })
}

// question is an item that takes answers, not approvals.
func question(id, swarm, handle string) bus.Item {
	it := item(id, swarm, handle)
	it.Kind, it.Action = bus.KindQuestion, bus.ActionNone
	return it
}
