package app

import (
	"os"
	"strings"
	"testing"
)

func TestCheckReportsIntegrityAndCounts(t *testing.T) {
	a := newAdmin(t)
	a.create("token", "create", "--name", "ci-1", "--swarm", "crew")
	code, out, errs := a.run("check")
	want := "integrity: ok\nitems: 0\ndecisions: 0\nrelays: 0\nevents: 0\nmessages: 0\ntokens: 1\n"
	if code != ExitOK || errs != "" || out != want {
		t.Fatalf("code=%d err=%q out=%q", code, errs, out)
	}
	// --db PATH overrides the environment.
	code, out2, _ := a.run("check", "--db", a.path)
	if code != ExitOK || out2 != want {
		t.Fatalf("--db: code=%d out=%q", code, out2)
	}
}

func TestCheckRefusesAMissingDatabaseAndCreatesNothing(t *testing.T) {
	a := newAdmin(t)
	for _, args := range [][]string{{"check"}, {"token", "list"}, {"token", "revoke", "--name", "ci-1"}, {"token", "revoke", "--desk-all"}} {
		code, out, errs := a.run(args...)
		if code != ExitFailed || out != "" || !strings.Contains(errs, "no database") {
			t.Fatalf("%v: code=%d out=%q err=%q", args, code, out, errs)
		}
		if _, err := os.Stat(a.path); err == nil {
			t.Fatalf("%v created the database", args)
		}
	}
}

func TestCheckUsage(t *testing.T) {
	a := newAdmin(t)
	for _, args := range [][]string{{"check", "extra"}, {"check", "--bogus"}} {
		if code, out, _ := a.run(args...); code != ExitUsage || out != "" {
			t.Fatalf("%v: code=%d out=%q", args, code, out)
		}
	}
}

func TestCheckReportsDamage(t *testing.T) {
	a := newAdmin(t)
	a.create("token", "create", "--name", "ci-1", "--swarm", "crew")
	// A file that is no database: the open fails and check says so with the store exit.
	if err := os.WriteFile(a.path, []byte("not a database, and long enough to be read as a header"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(a.path + "-wal"); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Remove(a.path + "-shm"); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if code, out, errs := a.run("check"); code != ExitStore || out != "" || errs == "" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
}
