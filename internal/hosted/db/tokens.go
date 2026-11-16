package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// ErrStarterCap is returned when a desk starter is asked for and the cap of live ones is reached.
var ErrStarterCap = errors.New("db: the desk starter cap is reached")

// The credential lookups below are by hash only, and they return revoked rows too: whether a
// row still counts is the verifier's call. None takes a token.

// NodeByHash returns the node token with hash h.
func (d *DB) NodeByHash(h string) (bus.Node, bool, error) {
	var n bus.Node
	var created, used, revoked string
	err := d.sql.QueryRowContext(context.Background(),
		`SELECT name, swarm, created, last_used, revoked FROM tokens WHERE hash = ? AND kind = 'node'`, h).
		Scan(&n.Name, &n.Swarm, &created, &used, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return bus.Node{}, false, nil
	}
	if err != nil {
		return bus.Node{}, false, fmt.Errorf("db: node by hash: %w", err)
	}
	if n.Created, n.LastUsed, n.Revoked, err = parseTimes(created, used, revoked); err != nil {
		return bus.Node{}, false, fmt.Errorf("db: node by hash: %w", err)
	}
	return n, true, nil
}

// StarterByHash returns the desk starter with hash h.
func (d *DB) StarterByHash(h string) (bus.Starter, bool, error) {
	var s bus.Starter
	var created, used, revoked string
	err := d.sql.QueryRowContext(context.Background(),
		`SELECT name, created, last_used, revoked FROM tokens WHERE hash = ? AND kind = 'desk-starter'`, h).
		Scan(&s.Name, &created, &used, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return bus.Starter{}, false, nil
	}
	if err != nil {
		return bus.Starter{}, false, fmt.Errorf("db: starter by hash: %w", err)
	}
	if s.Created, s.LastUsed, s.Revoked, err = parseTimes(created, used, revoked); err != nil {
		return bus.Starter{}, false, fmt.Errorf("db: starter by hash: %w", err)
	}
	return s, true, nil
}

// DeskByHash returns the desk human token with hash h.
func (d *DB) DeskByHash(h string) (bus.Desk, bool, error) {
	var dk bus.Desk
	var created, revoked string
	err := d.sql.QueryRowContext(context.Background(),
		`SELECT created, revoked FROM desk_tokens WHERE hash = ?`, h).Scan(&created, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return bus.Desk{}, false, nil
	}
	if err != nil {
		return bus.Desk{}, false, fmt.Errorf("db: desk by hash: %w", err)
	}
	if dk.Created, _, dk.Revoked, err = parseTimes(created, "", revoked); err != nil {
		return bus.Desk{}, false, fmt.Errorf("db: desk by hash: %w", err)
	}
	return dk, true, nil
}

// AccessByHash is Task 10's: the OAuth access store does not exist yet, so nothing is found.
func (d *DB) AccessByHash(string) (bus.Access, bool, error) { return bus.Access{}, false, nil }

// parseTimes parses three stored times; "" is the zero time.
func parseTimes(a, b, c string) (ta, tb, tc time.Time, err error) {
	for i, p := range []struct {
		in  string
		out *time.Time
	}{{a, &ta}, {b, &tb}, {c, &tc}} {
		if *p.out, err = parseTime(p.in); err != nil {
			return ta, tb, tc, fmt.Errorf("time %d: %w", i, err)
		}
	}
	return ta, tb, tc, nil
}

// TouchNode records that the live node token named name was used at at.
func (d *DB) TouchNode(name string, at time.Time) error {
	_, err := d.sql.ExecContext(context.Background(),
		`UPDATE tokens SET last_used = ? WHERE name = ? AND kind = 'node' AND revoked = ''`, fmtTime(at), name)
	if err != nil {
		return fmt.Errorf("db: touch node: %w", err)
	}
	return nil
}

// InsertNode stores a node token's hash. A live name already in use is a conflict (IsConflict).
func (d *DB) InsertNode(name, swarm, shown, hash string, at time.Time) error {
	_, err := d.sql.ExecContext(context.Background(),
		`INSERT INTO tokens (name, kind, swarm, shown, hash, created) VALUES (?, 'node', ?, ?, ?, ?)`,
		name, swarm, shown, hash, fmtTime(at))
	if err != nil {
		return fmt.Errorf("db: insert node token: %w", err)
	}
	return nil
}

// InsertStarter stores a desk starter's hash, unless maxLive starters are already live
// (ErrStarterCap). The count and the insert share one write transaction, so two commands at
// once cannot both pass the cap.
func (d *DB) InsertStarter(name, shown, hash string, at time.Time, maxLive int) error {
	ctx := context.Background()
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: insert desk starter: %w", err)
	}
	var live int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM tokens WHERE kind = 'desk-starter' AND revoked = ''`).Scan(&live); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("db: insert desk starter: %w", err)
	}
	if live >= maxLive {
		_ = tx.Rollback()
		return ErrStarterCap
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO tokens (name, kind, swarm, shown, hash, created) VALUES (?, 'desk-starter', '', ?, ?, ?)`,
		name, shown, hash, fmtTime(at)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("db: insert desk starter: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: insert desk starter: %w", err)
	}
	return nil
}

// InsertDesk stores a desk human token's hash (Task 10 mints them).
func (d *DB) InsertDesk(hash string, at time.Time) error {
	_, err := d.sql.ExecContext(context.Background(), `INSERT INTO desk_tokens (hash, created) VALUES (?, ?)`, hash, fmtTime(at))
	if err != nil {
		return fmt.Errorf("db: insert desk token: %w", err)
	}
	return nil
}

// ListTokens returns every node token and desk starter, oldest first. A row holds the first 8
// characters of its token and never its hash.
func (d *DB) ListTokens() ([]bus.TokenRow, error) {
	rows, err := d.sql.QueryContext(context.Background(),
		`SELECT kind, name, swarm, shown, created, last_used, revoked FROM tokens ORDER BY created, name`)
	if err != nil {
		return nil, fmt.Errorf("db: list tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []bus.TokenRow
	for rows.Next() {
		var r bus.TokenRow
		var kind, created, used, revoked string
		if err := rows.Scan(&kind, &r.Name, &r.Swarm, &r.Shown, &created, &used, &revoked); err != nil {
			return nil, fmt.Errorf("db: list tokens: %w", err)
		}
		r.Kind = bus.TokenKind(kind)
		if r.Created, r.LastUsed, r.Revoked, err = parseTimes(created, used, revoked); err != nil {
			return nil, fmt.Errorf("db: list tokens: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Revoke revokes the live node token or desk starter named name, and reports whether there was
// one. A token already revoked keeps its first revocation time.
func (d *DB) Revoke(name string, at time.Time) (bool, error) {
	res, err := d.sql.ExecContext(context.Background(),
		`UPDATE tokens SET revoked = ? WHERE name = ? AND revoked = ''`, fmtTime(at), name)
	if err != nil {
		return false, fmt.Errorf("db: revoke: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("db: revoke: %w", err)
	}
	return n > 0, nil
}

// RevokeDesk revokes every live desk starter and every live desk human token at once, and
// returns how many of each.
func (d *DB) RevokeDesk(at time.Time) (starters, desks int, err error) {
	ctx := context.Background()
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("db: revoke desk: %w", err)
	}
	for _, q := range []struct {
		sql string
		n   *int
	}{
		{`UPDATE tokens SET revoked = ? WHERE kind = 'desk-starter' AND revoked = ''`, &starters},
		{`UPDATE desk_tokens SET revoked = ? WHERE revoked = ''`, &desks},
	} {
		res, err := tx.ExecContext(ctx, q.sql, fmtTime(at))
		if err == nil {
			var n int64
			n, err = res.RowsAffected()
			*q.n = int(n)
		}
		if err != nil {
			_ = tx.Rollback()
			return 0, 0, fmt.Errorf("db: revoke desk: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("db: revoke desk: %w", err)
	}
	return starters, desks, nil
}

// RevokeDuet is Task 10's: with no OAuth stores yet there is no Duet token to revoke.
func (d *DB) RevokeDuet(time.Time) (int, error) { return 0, nil }
