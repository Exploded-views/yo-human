// Package app is yo-human-hosted's composition root: it reads the environment and runs a
// subcommand.
package app

import (
	"fmt"
	"io"

	"github.com/Exploded-views/yo-human/internal/version"
)

// Exit codes, with the CLI's meanings (sysexits style).
const (
	ExitOK    = 0
	ExitUsage = 64
	// ExitFailed is a refusal: the command was understood and would not run (a name in use, the
	// starter cap, no such token, no database).
	ExitFailed = 69
	// ExitStore is a database that would not open or answer.
	ExitStore = 74
	// ExitInternal is serve or selfcheck failing in a way the caller cannot fix by changing its
	// input: a listener that would not bind, a bus that did not answer as it should.
	ExitInternal = 70
	// ExitDegraded is `check` finding the database sound but delivery not healthy: the channel is
	// refused, a message was refused in the last 24 hours, or a pending message is overdue.
	ExitDegraded = 1
	// ExitConfig is a configuration the server refuses to start with.
	ExitConfig = 78
)

const usage = "usage: yo-human-hosted <command>\n\ncommands:\n  version    print the version\n  token      create, list and revoke node tokens and desk starters\n  check      check the database's integrity and count its rows\n  serve      run the MCP and approval listeners until SIGTERM or SIGINT\n  selfcheck  post an approval and a question, record Duet's words and read the answer, offline\n"

// Run executes yo-human-hosted with args (without the program name), reading configuration
// through env, and returns the exit code.
func Run(args []string, env func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return ExitUsage
	}
	switch args[0] {
	case "version":
		_, _ = fmt.Fprintf(stdout, "yo-human-hosted %s %s\n", version.Version, version.Commit)
		return ExitOK
	case "token":
		return runToken(args[1:], env, stdout, stderr)
	case "check":
		return admin{name: "check", env: env, stdout: stdout, stderr: stderr}.check(args[1:])
	case "serve":
		return runServe(args[1:], env, stdout, stderr)
	case "selfcheck":
		return runSelfcheck(args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "yo-human-hosted: unknown command %q\n\n%s", args[0], usage)
		return ExitUsage
	}
}
