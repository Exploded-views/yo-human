package db

import (
	"errors"
	"fmt"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// AppendEvent adds an event to the log and returns its seq.
func (x *txn) AppendEvent(e bus.Event) (int64, error) {
	res, err := x.tx.ExecContext(x.ctx, `INSERT INTO events (swarm, item_id, type, created, starter, phrase, user_code, expires)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, e.Swarm, e.ItemID, e.Type, fmtTime(e.Created), e.Starter, e.Phrase,
		e.UserCode, fmtTime(e.Expires))
	if err != nil {
		return 0, fmt.Errorf("db: append event: %w", err)
	}
	seq, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("db: append event: %w", err)
	}
	return seq, nil
}

// MaxEvents caps one EventsSince call.
const MaxEvents = 100

// EventsSince returns events with seq greater than since, in seq order, at most limit (above zero, clamped to MaxEvents). A swarm
// restricts the result to that swarm's events; an empty swarm means all of them.
func (x *txn) EventsSince(swarm string, since int64, limit int) ([]bus.Event, error) {
	if limit <= 0 {
		return nil, errors.New("db: EventsSince needs a limit above zero")
	}
	limit = min(limit, MaxEvents)
	q := `SELECT seq, swarm, item_id, type, created, starter, phrase, user_code, expires FROM events
		WHERE seq > ? AND (? = '' OR swarm = ?) ORDER BY seq LIMIT ?`
	rows, err := x.tx.QueryContext(x.ctx, q, since, swarm, swarm, limit)
	if err != nil {
		return nil, fmt.Errorf("db: read events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []bus.Event
	for rows.Next() {
		var e bus.Event
		var created, expires string
		if err := rows.Scan(&e.Seq, &e.Swarm, &e.ItemID, &e.Type, &created, &e.Starter, &e.Phrase, &e.UserCode, &expires); err != nil {
			return nil, fmt.Errorf("db: read events: %w", err)
		}
		if e.Created, err = parseTime(created); err != nil {
			return nil, fmt.Errorf("db: read events: %w", err)
		}
		if e.Expires, err = parseTime(expires); err != nil {
			return nil, fmt.Errorf("db: read events: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// InsertMessage adds a message to its thread.
func (x *txn) InsertMessage(m bus.Message) error {
	_, err := x.tx.ExecContext(x.ctx, `INSERT INTO messages (id, thread_id, from_name, from_kind, body, severity, created, urgency)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, m.ID, m.ThreadID, m.From, m.FromKind, m.Body, m.Severity, fmtTime(m.Created), m.Urgency)
	if err != nil {
		return fmt.Errorf("db: insert message: %w", err)
	}
	return nil
}

// Messages returns a thread's messages, oldest first.
func (x *txn) Messages(threadID string) ([]bus.Message, error) {
	rows, err := x.tx.QueryContext(x.ctx, `SELECT id, thread_id, from_name, from_kind, body, severity, created, urgency
		FROM messages WHERE thread_id = ? ORDER BY created, rowid`, threadID)
	if err != nil {
		return nil, fmt.Errorf("db: read messages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []bus.Message
	for rows.Next() {
		var m bus.Message
		var created string
		if err := rows.Scan(&m.ID, &m.ThreadID, &m.From, &m.FromKind, &m.Body, &m.Severity, &created, &m.Urgency); err != nil {
			return nil, fmt.Errorf("db: read messages: %w", err)
		}
		if m.Created, err = parseTime(created); err != nil {
			return nil, fmt.Errorf("db: read messages: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
