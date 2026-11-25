package db

import (
	"context"
	"fmt"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// NudgeState is the collapsed nudge's memory (one row, written by the migration).
func (x *txn) NudgeState() (bus.NudgeState, error) {
	var n bus.NudgeState
	var sent, failed string
	err := x.tx.QueryRowContext(x.ctx, `SELECT last_seq, last_sent, count, last_status, failed_at FROM nudge_state WHERE id = 1`).
		Scan(&n.LastSeq, &sent, &n.Count, &n.LastStatus, &failed)
	if err != nil {
		return bus.NudgeState{}, fmt.Errorf("db: read nudge state: %w", err)
	}
	if n.LastSent, err = parseTime(sent); err != nil {
		return bus.NudgeState{}, fmt.Errorf("db: read nudge state: %w", err)
	}
	if n.FailedAt, err = parseTime(failed); err != nil {
		return bus.NudgeState{}, fmt.Errorf("db: read nudge state: %w", err)
	}
	return n, nil
}

// PutNudgeState replaces the nudge's memory.
func (x *txn) PutNudgeState(n bus.NudgeState) error {
	_, err := x.tx.ExecContext(x.ctx, `UPDATE nudge_state SET last_seq = ?, last_sent = ?, count = ?, last_status = ?, failed_at = ? WHERE id = 1`,
		n.LastSeq, fmtTime(n.LastSent), n.Count, n.LastStatus, fmtTime(n.FailedAt))
	if err != nil {
		return fmt.Errorf("db: write nudge state: %w", err)
	}
	return nil
}

// Deferred returns the deferred items, soonest due first.
func (x *txn) Deferred() ([]bus.Deferred, error) {
	rows, err := x.tx.QueryContext(x.ctx, `SELECT item_id, due_at FROM nudge_deferred ORDER BY due_at, item_id`)
	if err != nil {
		return nil, fmt.Errorf("db: read deferred: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []bus.Deferred
	for rows.Next() {
		var d bus.Deferred
		var due string
		if err := rows.Scan(&d.ItemID, &due); err != nil {
			return nil, fmt.Errorf("db: read deferred: %w", err)
		}
		if d.DueAt, err = parseTime(due); err != nil {
			return nil, fmt.Errorf("db: read deferred: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// PutDeferred inserts the item, or replaces its due time.
func (x *txn) PutDeferred(d bus.Deferred) error {
	_, err := x.tx.ExecContext(x.ctx, `INSERT INTO nudge_deferred (item_id, due_at) VALUES (?, ?)
		ON CONFLICT (item_id) DO UPDATE SET due_at = excluded.due_at`, d.ItemID, fmtTime(d.DueAt))
	if err != nil {
		return fmt.Errorf("db: write deferred: %w", err)
	}
	return nil
}

// DeleteDeferred removes the item; an item that isn't deferred is no error.
func (x *txn) DeleteDeferred(itemID string) error {
	if _, err := x.tx.ExecContext(x.ctx, `DELETE FROM nudge_deferred WHERE item_id = ?`, itemID); err != nil {
		return fmt.Errorf("db: delete deferred: %w", err)
	}
	return nil
}

// BrokenFlag is the sticky mark that the channel is refused.
func (x *txn) BrokenFlag() (bus.BrokenFlag, error) {
	var f bus.BrokenFlag
	var broken int
	var since string
	if err := x.tx.QueryRowContext(x.ctx, `SELECT broken, since, status FROM delivery_flag WHERE id = 1`).Scan(&broken, &since, &f.Status); err != nil {
		return bus.BrokenFlag{}, fmt.Errorf("db: read delivery flag: %w", err)
	}
	var err error
	if f.Since, err = parseTime(since); err != nil {
		return bus.BrokenFlag{}, fmt.Errorf("db: read delivery flag: %w", err)
	}
	f.Raised = broken == 1
	return f, nil
}

// PutBrokenFlag replaces the mark.
func (x *txn) PutBrokenFlag(f bus.BrokenFlag) error {
	_, err := x.tx.ExecContext(x.ctx, `UPDATE delivery_flag SET broken = ?, since = ?, status = ? WHERE id = 1`,
		boolInt(f.Raised), fmtTime(f.Since), f.Status)
	if err != nil {
		return fmt.Errorf("db: write delivery flag: %w", err)
	}
	return nil
}

// DeliveryState is nil while delivery is healthy: the channel is not broken, no message was
// refused within bus.DeliveryFailedWindow, and no pending row is more than bus.DeliveryOverdue
// past its time.
func (x *txn) DeliveryState(now time.Time) (*bus.DeliveryState, error) {
	flag, err := x.BrokenFlag()
	if err != nil {
		return nil, err
	}
	ds := &bus.DeliveryState{Broken: flag.Raised, Since: flag.Since}
	if flag.Raised {
		ds.Status = flag.Status
	}
	if err := x.countFailed(ds, fmtTime(now.Add(-bus.DeliveryFailedWindow))); err != nil {
		return nil, err
	}
	var overdue int
	err = x.tx.QueryRowContext(x.ctx, `SELECT count(*), coalesce(sum(next_at < ?), 0) FROM outbox WHERE state = 'pending'`,
		fmtTime(now.Add(-bus.DeliveryOverdue))).Scan(&ds.Pending, &overdue)
	if err != nil {
		return nil, fmt.Errorf("db: count pending: %w", err)
	}
	if !ds.Broken && ds.Failed == 0 && overdue == 0 {
		return nil, nil //nolint:nilnil // nil is the documented healthy answer
	}
	return ds, nil
}

// countFailed counts the messages refused after cutoff, a row of the outbox or the nudge, and
// takes the status of the latest as ds.Status unless the channel is broken.
func (x *txn) countFailed(ds *bus.DeliveryState, cutoff string) error {
	rows, err := x.tx.QueryContext(x.ctx, `SELECT failed_at, last_status FROM outbox WHERE state = 'failed' AND failed_at > ?
		UNION ALL SELECT failed_at, last_status FROM nudge_state WHERE failed_at > ?
		ORDER BY failed_at DESC`, cutoff, cutoff)
	if err != nil {
		return fmt.Errorf("db: count failed: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var at string
		var status int
		if err := rows.Scan(&at, &status); err != nil {
			return fmt.Errorf("db: count failed: %w", err)
		}
		if ds.Failed++; ds.Failed == 1 && !ds.Broken {
			ds.Status = status
		}
	}
	return rows.Err()
}

// Delivery is DeliveryState read outside a transaction of the caller's, for `check`.
func (d *DB) Delivery(ctx context.Context, now time.Time) (ds *bus.DeliveryState, err error) {
	err = d.Tx(ctx, func(x bus.Tx) (err error) { ds, err = x.DeliveryState(now); return })
	return ds, err
}
