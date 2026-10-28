package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

const itemCols = `id, swarm, handle, kind, severity, to_target, from_name, from_kind, title, summary, detail,
	required_action, repo, ref, base_ref, target, pinned_commit, pinned_tree, action, link, risk_flags,
	untrusted, revision, state, content_hash, last_seq, acked_seq, created, updated`

// rowScanner is a *sql.Row or *sql.Rows.
type rowScanner interface{ Scan(dest ...any) error }

func scanItem(r rowScanner) (bus.Item, error) {
	var it bus.Item
	var flags, created, updated string
	var untrusted int
	err := r.Scan(&it.ID, &it.Swarm, &it.Handle, &it.Kind, &it.Severity, &it.To, &it.From, &it.FromKind,
		&it.Title, &it.Summary, &it.Detail, &it.RequiredAction, &it.Repo, &it.Ref, &it.BaseRef, &it.Target,
		&it.PinnedCommit, &it.PinnedTree, &it.Action, &it.Link, &flags, &untrusted, &it.Revision, &it.State,
		&it.ContentHash, &it.LastSeq, &it.AckedSeq, &created, &updated)
	if err != nil {
		return bus.Item{}, err
	}
	if err := json.Unmarshal([]byte(flags), &it.RiskFlags); err != nil {
		return bus.Item{}, fmt.Errorf("risk flags: %w", err)
	}
	if len(it.RiskFlags) == 0 {
		it.RiskFlags = nil
	}
	it.Untrusted = untrusted == 1
	if it.Created, err = parseTime(created); err != nil {
		return bus.Item{}, err
	}
	it.Updated, err = parseTime(updated)
	return it, err
}

func (x *txn) oneItem(where string, args ...any) (bus.Item, bool, error) {
	it, err := scanItem(x.tx.QueryRowContext(x.ctx, "SELECT "+itemCols+" FROM items WHERE "+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return bus.Item{}, false, nil
	}
	if err != nil {
		return bus.Item{}, false, fmt.Errorf("db: read item: %w", err)
	}
	return it, true, nil
}

func (x *txn) ItemByID(id string) (bus.Item, bool, error) { return x.oneItem("id = ?", id) }

func (x *txn) ItemByHandle(swarm, handle string) (bus.Item, bool, error) {
	return x.oneItem("swarm = ? AND handle = ?", swarm, handle)
}

func flagsJSON(flags []string) (string, error) {
	if flags == nil {
		flags = []string{}
	}
	b, err := json.Marshal(flags)
	return string(b), err
}

func (x *txn) InsertItem(it bus.Item) error {
	flags, err := flagsJSON(it.RiskFlags)
	if err != nil {
		return fmt.Errorf("db: insert item: %w", err)
	}
	_, err = x.tx.ExecContext(x.ctx, `INSERT INTO items (`+itemCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.ID, it.Swarm, it.Handle, it.Kind, it.Severity, it.To, it.From, it.FromKind, it.Title, it.Summary,
		it.Detail, it.RequiredAction, it.Repo, it.Ref, it.BaseRef, it.Target, it.PinnedCommit, it.PinnedTree,
		it.Action, it.Link, flags, boolInt(it.Untrusted), it.Revision, it.State, it.ContentHash, it.LastSeq,
		it.AckedSeq, fmtTime(it.Created), fmtTime(it.Updated))
	if err != nil {
		return fmt.Errorf("db: insert item: %w", err)
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// UpdateItem rewrites the item's mutable fields. ID, swarm, author and creation time never change.
func (x *txn) UpdateItem(it bus.Item) error {
	flags, err := flagsJSON(it.RiskFlags)
	if err != nil {
		return fmt.Errorf("db: update item: %w", err)
	}
	res, err := x.tx.ExecContext(x.ctx, `UPDATE items SET handle = ?, kind = ?, severity = ?, to_target = ?, title = ?,
		summary = ?, detail = ?, required_action = ?, repo = ?, ref = ?, base_ref = ?, target = ?, pinned_commit = ?,
		pinned_tree = ?, action = ?, link = ?, risk_flags = ?, untrusted = ?, revision = ?, state = ?,
		content_hash = ?, last_seq = ?, acked_seq = ?, updated = ? WHERE id = ?`,
		it.Handle, it.Kind, it.Severity, it.To, it.Title, it.Summary, it.Detail, it.RequiredAction, it.Repo,
		it.Ref, it.BaseRef, it.Target, it.PinnedCommit, it.PinnedTree, it.Action, it.Link, flags,
		boolInt(it.Untrusted), it.Revision, it.State, it.ContentHash, it.LastSeq, it.AckedSeq,
		fmtTime(it.Updated), it.ID)
	if err != nil {
		return fmt.Errorf("db: update item: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return fmt.Errorf("db: update item: %w", ErrNotFound)
	}
	return nil
}

// ListOpenForHuman returns the open items addressed to the human, oldest first.
func (x *txn) ListOpenForHuman() ([]bus.Item, error) {
	rows, err := x.tx.QueryContext(x.ctx, "SELECT "+itemCols+" FROM items WHERE state = 'open' AND to_target = 'human' ORDER BY created, id")
	if err != nil {
		return nil, fmt.Errorf("db: list open items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []bus.Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("db: list open items: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
