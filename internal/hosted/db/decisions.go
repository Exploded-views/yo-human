package db

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// Decision returns the effective record for one revision: its human record if there is one,
// else its agent record.
func (x *txn) Decision(itemID string, rev int) (bus.Record, bool, error) {
	var r bus.Record
	var humanTS, recorded string
	err := x.tx.QueryRowContext(x.ctx, `SELECT item_id, revision, kind, decision, actor_kind, text, source, by_name,
		source_message, human_ts, recorded FROM decisions WHERE item_id = ? AND revision = ?
		ORDER BY CASE actor_kind WHEN 'human' THEN 0 ELSE 1 END LIMIT 1`, itemID, rev).
		Scan(&r.ItemID, &r.Revision, &r.Kind, &r.Decision, &r.ActorKind, &r.Text, &r.Source, &r.By,
			&r.SourceMessage, &humanTS, &recorded)
	if errors.Is(err, sql.ErrNoRows) {
		return bus.Record{}, false, nil
	}
	if err != nil {
		return bus.Record{}, false, fmt.Errorf("db: read decision: %w", err)
	}
	if r.HumanTS, err = parseTime(humanTS); err != nil {
		return bus.Record{}, false, fmt.Errorf("db: read decision: %w", err)
	}
	if r.Recorded, err = parseTime(recorded); err != nil {
		return bus.Record{}, false, fmt.Errorf("db: read decision: %w", err)
	}
	return r, true, nil
}

// InsertDecision stores an answer or approval and, in the same transaction, marks the item
// decided if the record is for its current revision. That is the only way an item becomes
// decided. A second record for the same (item, revision, actor kind) is a conflict
// (IsConflict), and the database refuses an agent approval.
func (x *txn) InsertDecision(r bus.Record) error {
	_, err := x.tx.ExecContext(x.ctx, `INSERT INTO decisions (item_id, revision, kind, decision, actor_kind, text,
		source, by_name, source_message, human_ts, recorded) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ItemID, r.Revision, r.Kind, r.Decision, r.ActorKind, r.Text, r.Source, r.By, r.SourceMessage,
		fmtTime(r.HumanTS), fmtTime(r.Recorded))
	if err != nil {
		return fmt.Errorf("db: insert decision: %w", err)
	}
	if _, err := x.tx.ExecContext(x.ctx, "UPDATE items SET state = 'decided' WHERE id = ? AND revision = ?", r.ItemID, r.Revision); err != nil {
		return fmt.Errorf("db: mark item decided: %w", err)
	}
	return nil
}

// InsertRelay stores the owner's words as a node relayed them. It never changes the item.
func (x *txn) InsertRelay(r bus.Relay) error {
	_, err := x.tx.ExecContext(x.ctx, `INSERT INTO relays (id, item_id, revision, text, source, by_name, human_ts, recorded)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, r.ID, r.ItemID, r.Revision, r.Text, r.Source, r.By,
		fmtTime(r.HumanTS), fmtTime(r.Recorded))
	if err != nil {
		return fmt.Errorf("db: insert relay: %w", err)
	}
	return nil
}

// Relays returns an item's relays, oldest first.
func (x *txn) Relays(itemID string) ([]bus.Relay, error) {
	rows, err := x.tx.QueryContext(x.ctx, `SELECT id, item_id, revision, text, source, by_name, human_ts, recorded
		FROM relays WHERE item_id = ? ORDER BY recorded, rowid`, itemID)
	if err != nil {
		return nil, fmt.Errorf("db: read relays: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []bus.Relay
	for rows.Next() {
		var r bus.Relay
		var humanTS, recorded string
		if err := rows.Scan(&r.ID, &r.ItemID, &r.Revision, &r.Text, &r.Source, &r.By, &humanTS, &recorded); err != nil {
			return nil, fmt.Errorf("db: read relays: %w", err)
		}
		if r.HumanTS, err = parseTime(humanTS); err != nil {
			return nil, fmt.Errorf("db: read relays: %w", err)
		}
		if r.Recorded, err = parseTime(recorded); err != nil {
			return nil, fmt.Errorf("db: read relays: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
