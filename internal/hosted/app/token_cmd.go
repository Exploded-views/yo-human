package app

import (
	"fmt"
	"io"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/auth"
	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

const verbUsage = "usage: yo-human-hosted token <create|create-desk-starter|list|revoke> [flags]"

// runToken runs `token <verb>`.
func runToken(args []string, env func(string) string, stdout, stderr io.Writer) int {
	a := admin{name: "token", env: env, stdout: stdout, stderr: stderr}
	if len(args) == 0 {
		return a.usageErr(verbUsage)
	}
	a.name = "token " + args[0]
	switch args[0] {
	case "create":
		return a.create(args[1:])
	case "create-desk-starter":
		return a.createStarter(args[1:])
	case "list":
		return a.list(args[1:])
	case "revoke":
		return a.revoke(args[1:])
	}
	return a.usageErr(verbUsage)
}

// create is `token create --name N --swarm S`: it prints the token, and only the token.
func (a admin) create(args []string) int {
	fs, path := a.flags()
	name, swarm := fs.String("name", "", "the node's `name`"), fs.String("swarm", "", "the swarm the node speaks for")
	if code, stop := a.parse(fs, args); stop {
		return code
	}
	if !bus.ValidName(*name) || !bus.ValidSwarm(*swarm) {
		return a.usageErr("--name and --swarm are required: lowercase letters, digits and hyphens, at most 63 characters, and the swarm not duet or human")
	}
	return a.withDB(dbPath(a.env, *path), false, func(d *db.DB) error {
		token, hash, _ := auth.Mint(auth.PrefixNode) // a fixed, valid prefix: Mint cannot fail
		if err := d.InsertNode(*name, *swarm, token[:8], hash, now()); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(a.stdout, token)
		return nil
	})
}

// createStarter is `token create-desk-starter --name N`.
func (a admin) createStarter(args []string) int {
	fs, path := a.flags()
	name := fs.String("name", "", "the starter's `name`")
	if code, stop := a.parse(fs, args); stop {
		return code
	}
	if !bus.ValidName(*name) {
		return a.usageErr("--name is required: lowercase letters, digits and hyphens, at most 63 characters")
	}
	return a.withDB(dbPath(a.env, *path), false, func(d *db.DB) error {
		token, hash, _ := auth.Mint(auth.PrefixStarter) // a fixed, valid prefix: Mint cannot fail
		if err := d.InsertStarter(*name, token[:8], hash, now(), maxLiveStarters); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(a.stdout, token)
		return nil
	})
}

// list is `token list`: name, kind, swarm, the first 8 characters, created, last used and
// revoked, tab-separated. Never a hash or a token.
func (a admin) list(args []string) int {
	fs, path := a.flags()
	if code, stop := a.parse(fs, args); stop {
		return code
	}
	return a.withDB(dbPath(a.env, *path), true, func(d *db.DB) error {
		rows, err := d.ListTokens()
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(a.stdout, "NAME\tKIND\tSWARM\tPREFIX\tCREATED\tLAST_USED\tREVOKED")
		for _, r := range rows {
			_, _ = fmt.Fprintf(a.stdout, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.Name, r.Kind, orDash(r.Swarm), r.Shown, stamp(r.Created), stamp(r.LastUsed), stamp(r.Revoked))
		}
		return nil
	})
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

// revoke is `token revoke` with exactly one of --name, --desk-all and --duet-all.
func (a admin) revoke(args []string) int {
	fs, path := a.flags()
	name := fs.String("name", "", "revoke the node or desk starter with this `name`")
	deskAll := fs.Bool("desk-all", false, "revoke every desk token and every desk starter")
	duetAll := fs.Bool("duet-all", false, "revoke every Duet access and refresh token")
	if code, stop := a.parse(fs, args); stop {
		return code
	}
	if !oneChosen(*name, *deskAll, *duetAll) {
		return a.usageErr("give exactly one of --name N (a valid name), --desk-all and --duet-all")
	}
	return a.withDB(dbPath(a.env, *path), true, func(d *db.DB) error { return revokeOp(d, *name, *deskAll, *duetAll) })
}

// oneChosen reports whether exactly one of the three revoke selectors is set, and a name is a
// valid one.
func oneChosen(name string, deskAll, duetAll bool) bool {
	chosen := 0
	for _, set := range []bool{name != "", deskAll, duetAll} {
		if set {
			chosen++
		}
	}
	return chosen == 1 && (name == "" || bus.ValidName(name))
}

func revokeOp(d *db.DB, name string, deskAll, duetAll bool) (err error) {
	switch {
	case deskAll:
		_, _, err = d.RevokeDesk(now())
	case duetAll:
		_, err = d.RevokeDuet(now())
	default:
		var ok bool
		if ok, err = d.Revoke(name, now()); err == nil && !ok {
			err = errNoLiveToken
		}
	}
	return err
}
