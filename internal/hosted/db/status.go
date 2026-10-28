package db

import (
	"encoding/json"
	"fmt"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// PutStatus stores a swarm's status document, replacing any earlier one.
func (x *txn) PutStatus(s bus.StatusDoc) error {
	_, err := x.tx.ExecContext(x.ctx, `INSERT INTO statuses (swarm, doc, updated) VALUES (?, ?, ?)
		ON CONFLICT (swarm) DO UPDATE SET doc = excluded.doc, updated = excluded.updated`,
		s.Swarm, string(s.Doc), fmtTime(s.Updated))
	if err != nil {
		return fmt.Errorf("db: put status: %w", err)
	}
	return nil
}

// Statuses returns one swarm's status document, or every swarm's (by swarm name) for "".
func (x *txn) Statuses(swarm string) ([]bus.StatusDoc, error) {
	rows, err := x.tx.QueryContext(x.ctx, `SELECT swarm, doc, updated FROM statuses
		WHERE ? = '' OR swarm = ? ORDER BY swarm`, swarm, swarm)
	if err != nil {
		return nil, fmt.Errorf("db: read statuses: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []bus.StatusDoc
	for rows.Next() {
		var s bus.StatusDoc
		var doc, updated string
		if err := rows.Scan(&s.Swarm, &doc, &updated); err != nil {
			return nil, fmt.Errorf("db: read statuses: %w", err)
		}
		s.Doc = json.RawMessage(doc)
		if s.Updated, err = parseTime(updated); err != nil {
			return nil, fmt.Errorf("db: read statuses: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
