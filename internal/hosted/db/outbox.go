package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

const outboxCols = `id, kind, swarm, item_id, revision, level, title, body, state, attempts, next_at, created,
	sent_at, failed_at, last_status, retried`

func scanOutbox(r rowScanner) (bus.OutboxRow, error) {
	var o bus.OutboxRow
	var next, created, sent, failed string
	var retried int
	err := r.Scan(&o.ID, &o.Kind, &o.Swarm, &o.ItemID, &o.Revision, &o.Level, &o.Title, &o.Body, &o.State, &o.Attempts,
		&next, &created, &sent, &failed, &o.LastStatus, &retried)
	if err != nil {
		return bus.OutboxRow{}, err
	}
	o.Retried = retried == 1
	for _, f := range []struct {
		raw string
		dst *time.Time
	}{{next, &o.NextAt}, {created, &o.Created}, {sent, &o.SentAt}, {failed, &o.FailedAt}} {
		if *f.dst, err = parseTime(f.raw); err != nil {
			return bus.OutboxRow{}, err
		}
	}
	return o, nil
}

// InsertOutbox stores a row and returns its id.
func (x *txn) InsertOutbox(o bus.OutboxRow) (int64, error) {
	res, err := x.tx.ExecContext(x.ctx, `INSERT INTO outbox (kind, swarm, item_id, revision, level, title, body, state, attempts,
		next_at, created, sent_at, failed_at, last_status, retried) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		o.Kind, o.Swarm, o.ItemID, o.Revision, o.Level, o.Title, o.Body, o.State, o.Attempts, fmtTime(o.NextAt),
		fmtTime(o.Created), fmtTime(o.SentAt), fmtTime(o.FailedAt), o.LastStatus, boolInt(o.Retried))
	if err != nil {
		return 0, fmt.Errorf("db: insert outbox: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("db: insert outbox: %w", err)
	}
	return id, nil
}

// UpdateOutbox rewrites a row's progress: its state, attempts, times, status and retry mark.
func (x *txn) UpdateOutbox(o bus.OutboxRow) error {
	res, err := x.tx.ExecContext(x.ctx, `UPDATE outbox SET level = ?, state = ?, attempts = ?, next_at = ?, sent_at = ?,
		failed_at = ?, last_status = ?, retried = ? WHERE id = ?`, o.Level, o.State, o.Attempts, fmtTime(o.NextAt),
		fmtTime(o.SentAt), fmtTime(o.FailedAt), o.LastStatus, boolInt(o.Retried), o.ID)
	if err != nil {
		return fmt.Errorf("db: update outbox: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return fmt.Errorf("db: update outbox: %w", ErrNotFound)
	}
	return nil
}

func (x *txn) queryOutbox(where string, args ...any) ([]bus.OutboxRow, error) {
	rows, err := x.tx.QueryContext(x.ctx, "SELECT "+outboxCols+" FROM outbox WHERE "+where, args...) //nolint:gosec // where is a literal from this file
	if err != nil {
		return nil, fmt.Errorf("db: read outbox: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []bus.OutboxRow
	for rows.Next() {
		o, err := scanOutbox(rows)
		if err != nil {
			return nil, fmt.Errorf("db: read outbox: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// PendingOutbox returns the pending rows that are due, oldest first.
func (x *txn) PendingOutbox(now time.Time, limit int) ([]bus.OutboxRow, error) {
	return x.queryOutbox(`state = 'pending' AND next_at <= ? ORDER BY created, id LIMIT ?`, fmtTime(now), limit)
}

// FailedSince returns the failed rows whose failure is not before since.
func (x *txn) FailedSince(since time.Time) ([]bus.OutboxRow, error) {
	return x.queryOutbox(`state = 'failed' AND failed_at >= ? ORDER BY id`, fmtTime(since))
}

// AllOutbox returns every row, in id order.
func (x *txn) AllOutbox() ([]bus.OutboxRow, error) { return x.queryOutbox(`1 = 1 ORDER BY id`) }

// LastPush is the newest ledger time for the item, at any revision.
func (x *txn) LastPush(itemID string) (time.Time, bool, error) {
	return x.latest(`SELECT max(at) FROM owner_pushes WHERE item_id = ?`, itemID)
}

// LastPageSent is the newest send time of a page row of the swarm.
func (x *txn) LastPageSent(swarm string) (time.Time, bool, error) {
	return x.latest(`SELECT max(sent_at) FROM outbox WHERE kind = 'page' AND state = 'sent' AND swarm = ?`, swarm)
}

// LastPageSentForItem is the newest send time of a page row for the item.
func (x *txn) LastPageSentForItem(string) (time.Time, bool, error) { return time.Time{}, false, nil }

// latest reads one nullable time: ok is false when nothing matched.
func (x *txn) latest(query string, args ...any) (time.Time, bool, error) {
	var s sql.NullString
	if err := x.tx.QueryRowContext(x.ctx, query, args...).Scan(&s); err != nil {
		return time.Time{}, false, fmt.Errorf("db: read ledger: %w", err)
	}
	if !s.Valid || s.String == "" {
		return time.Time{}, false, nil
	}
	t, err := parseTime(s.String)
	return t, err == nil, err
}

// InsertPush adds a ledger row; a second one for the same source, id and item is a conflict.
func (x *txn) InsertPush(p bus.Push) error {
	_, err := x.tx.ExecContext(x.ctx, `INSERT INTO owner_pushes (source, source_id, item_id, revision, at) VALUES (?, ?, ?, ?, ?)`,
		p.Source, p.SourceID, p.ItemID, p.Revision, fmtTime(p.At))
	if err != nil {
		return fmt.Errorf("db: insert push: %w", err)
	}
	return nil
}

// Pushes returns the item's ledger rows, oldest first.
func (x *txn) Pushes(itemID string) ([]bus.Push, error) {
	rows, err := x.tx.QueryContext(x.ctx, `SELECT source, source_id, item_id, revision, at FROM owner_pushes WHERE item_id = ? ORDER BY at, id`, itemID)
	if err != nil {
		return nil, fmt.Errorf("db: read pushes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []bus.Push
	for rows.Next() {
		var p bus.Push
		var at string
		if err := rows.Scan(&p.Source, &p.SourceID, &p.ItemID, &p.Revision, &at); err != nil {
			return nil, fmt.Errorf("db: read pushes: %w", err)
		}
		if p.At, err = parseTime(at); err != nil {
			return nil, fmt.Errorf("db: read pushes: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
