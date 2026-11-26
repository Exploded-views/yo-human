package app

import (
	"context"
	"fmt"
	"io"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

const outboxUsage = "usage: yo-human-hosted outbox test [--db PATH]"

// runOutbox runs `outbox <verb>`. The one verb is test.
func runOutbox(args []string, env func(string) string, stdout, stderr io.Writer) int {
	a := admin{name: "outbox", env: env, stdout: stdout, stderr: stderr}
	if len(args) == 0 || args[0] != "test" {
		return a.usageErr(outboxUsage)
	}
	a.name = "outbox test"
	return a.outboxTest(args[1:])
}

// outboxTest is `outbox test [--db PATH]`: it queues one test push, which the next probe sends.
// Its 2xx is how an operator clears the broken-channel flag after fixing the key.
func (a admin) outboxTest(args []string) int {
	fs, path := a.flags()
	if code, stop := a.parse(fs, args); stop {
		return code
	}
	return a.withDB(dbPath(a.env, *path), true, func(d *db.DB) error {
		at := now()
		err := d.Tx(context.Background(), func(x bus.Tx) error {
			_, err := x.InsertOutbox(bus.OutboxRow{Kind: bus.OutboxTest, Level: "passive", Title: "yo-human", Body: "test push",
				State: bus.OutboxPending, NextAt: at, Created: at})
			return err
		})
		if err == nil {
			_, _ = fmt.Fprintln(a.stdout, "queued a test push; the running serve sends it on its next tick")
		}
		return err
	})
}
