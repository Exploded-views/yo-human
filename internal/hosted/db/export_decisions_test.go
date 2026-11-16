package db

import (
	"context"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// RowsRecordedAt returns an item's decision rows recorded at t: how Task 7 resolves an event's
// Created to the row it records.
func (d *DB) RowsRecordedAt(ctx context.Context, itemID string, t time.Time) ([]bus.Record, error) {
	return d.decisionRows(ctx, "item_id = ? AND recorded = ?", itemID, fmtTime(t))
}

// RowsOfRevision returns every decision row of one revision, agent row first.
func (d *DB) RowsOfRevision(ctx context.Context, itemID string, rev int) ([]bus.Record, error) {
	return d.decisionRows(ctx, "item_id = ? AND revision = ?", itemID, rev)
}

func (d *DB) decisionRows(ctx context.Context, where string, args ...any) ([]bus.Record, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT item_id, revision, kind, decision, actor_kind, text, source, by_name, source_message
		FROM decisions WHERE `+where+` ORDER BY actor_kind`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []bus.Record
	for rows.Next() {
		var r bus.Record
		if err := rows.Scan(&r.ItemID, &r.Revision, &r.Kind, &r.Decision, &r.ActorKind, &r.Text, &r.Source, &r.By, &r.SourceMessage); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
