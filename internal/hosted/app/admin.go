package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

// admin is one admin command's streams and environment. The admin commands read only
// YH_DB_PATH, never the server's full configuration, so they run where the GitHub and Bark
// settings are not set.
type admin struct {
	name           string
	env            func(string) string
	stdout, stderr io.Writer
}

// errNoLiveToken is what `token revoke --name` finds when no live token has the name.
var errNoLiveToken = errors.New("no live token has that name")

// errDamaged is what `check` returns when SQLite's integrity check reports problems; the counts
// are still printed first.
var errDamaged = errors.New("the database failed its integrity check")

// maxLiveStarters is the cap on live desk starters.
const maxLiveStarters = 2

func (a admin) say(format string, args ...any) {
	_, _ = fmt.Fprintf(a.stderr, "yo-human-hosted: "+format+"\n", args...)
}

func (a admin) usageErr(msg string) int {
	a.say("%s: %s", a.name, msg)
	return ExitUsage
}

// parse reads fs from args. It returns the exit code and whether to stop: -h prints the flags
// and stops with ExitOK; a bad flag or a stray argument is a usage error.
func (a admin) parse(fs *flag.FlagSet, args []string) (code int, stop bool) {
	fs.SetOutput(io.Discard)
	err := fs.Parse(args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		a.say("usage: %s", a.name)
		fs.SetOutput(a.stderr)
		fs.PrintDefaults()
		return ExitOK, true
	case err != nil:
		return a.usageErr(err.Error()), true
	case fs.NArg() > 0:
		return a.usageErr("takes no arguments beyond its flags"), true
	}
	return ExitOK, false
}

// flags returns a flag set for the command with the --db flag, and the path flag's value.
func (a admin) flags() (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(a.name, flag.ContinueOnError)
	return fs, fs.String("db", "", "the database `path` (default $YH_DB_PATH, else /data/bus.db)")
}

// withDB opens the database and runs op on it, and turns what op returns into an exit code.
// With mustExist, a missing database is refused rather than created.
func (a admin) withDB(path string, mustExist bool, op func(*db.DB) error) int {
	if _, err := os.Stat(path); mustExist && err != nil {
		a.say("no database at the configured path (or it cannot be read); nothing was created")
		return ExitFailed
	}
	d, err := db.Open(path)
	if err != nil {
		a.say("%v", err)
		return ExitStore
	}
	defer func() { _ = d.Close() }()
	return a.result(op(d))
}

func (a admin) result(err error) int {
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, db.ErrStarterCap):
		a.say("%d desk starters are already live; revoke one first (token revoke --name N)", maxLiveStarters)
		return ExitFailed
	case db.IsConflict(err):
		a.say("a live token with that name already exists")
		return ExitFailed
	case errors.Is(err, errNoLiveToken):
		a.say("%v", err)
		return ExitFailed
	default:
		a.say("%v", err)
		return ExitStore
	}
}

func now() time.Time { return time.Now().UTC() }
