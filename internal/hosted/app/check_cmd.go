package app

import (
	"context"
	"fmt"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
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
		ds, err := d.Delivery(context.Background(), now())
		if err != nil || ds == nil {
			return err
		}
		a.printDelivery(ds)
		return errDegraded
	})
}

// printDelivery says why delivery is not healthy, with the same counts Duet's views carry.
func (a admin) printDelivery(ds *bus.DeliveryState) {
	why := "stuck"
	switch {
	case ds.Broken:
		why = "broken"
	case ds.Failed > 0:
		why = "refused"
	}
	_, _ = fmt.Fprintf(a.stdout, "delivery: %s\n", why)
	if ds.Broken {
		_, _ = fmt.Fprintf(a.stdout, "delivery_since: %s\n", ds.Since.UTC().Format(time.RFC3339))
	}
	_, _ = fmt.Fprintf(a.stdout, "delivery_status: %d\ndelivery_failed: %d\ndelivery_pending: %d\n", ds.Status, ds.Failed, ds.Pending)
}
