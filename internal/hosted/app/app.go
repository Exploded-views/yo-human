// Package app is yo-human-hosted's composition root: it reads the environment and runs a
// subcommand.
package app

import (
	"fmt"
	"io"

	"github.com/Exploded-views/yo-human/internal/version"
)

// Exit codes, with the CLI's meanings (sysexits style). 70 (internal) and 78 (configuration)
// arrive with serve (Task 12), the first command that can fail those ways.
const (
	ExitOK    = 0
	ExitUsage = 64
)

const usage = "usage: yo-human-hosted <command>\n\ncommands:\n  version  print the version\n"

// Run executes yo-human-hosted with args (without the program name), reading configuration
// through env, and returns the exit code.
func Run(args []string, env func(string) string, stdout, stderr io.Writer) int {
	_ = env // no subcommand reads configuration yet
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return ExitUsage
	}
	switch args[0] {
	case "version":
		_, _ = fmt.Fprintf(stdout, "yo-human-hosted %s %s\n", version.Version, version.Commit)
		return ExitOK
	default:
		_, _ = fmt.Fprintf(stderr, "yo-human-hosted: unknown command %q\n\n%s", args[0], usage)
		return ExitUsage
	}
}
