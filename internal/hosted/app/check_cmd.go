package app

import (
	"context"
	"fmt"

	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

// check is `check [--db PATH]`: SQLite's integrity verdict and the row counts.
func (a admin) check(args []string) int {
	fs, path := a.flags()
	if code, stop := a.parse(fs, args); stop {
		return code
	}
	return a.withDB(dbPath(a.env, *path), true, func(d *db.DB) error {
		c, err := d.Check(context.Background())
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(a.stdout, "integrity: %s\nitems: %d\ndecisions: %d\nrelays: %d\nevents: %d\nmessages: %d\ntokens: %d\n",
			c.Integrity, c.Items, c.Decisions, c.Relays, c.Events, c.Messages, c.Tokens)
		if c.Integrity != "ok" {
			return errDamaged
		}
		return nil
	})
}
