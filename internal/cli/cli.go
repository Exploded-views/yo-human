// Package cli parses arguments and maps results to exit codes.
package cli

import (
	"fmt"
	"io"

	"github.com/Exploded-views/yo-human/internal/version"
)

const usage = "usage: yo-human <command>\n\ncommands:\n  version  print the version\n"

// Run executes yo-human with args (without the program name) and returns the exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return ExitUsage
	}
	switch args[0] {
	case "version":
		_, _ = fmt.Fprintf(stdout, "yo-human %s %s\n", version.Version, version.Commit)
		return ExitOK
	default:
		_, _ = fmt.Fprintf(stderr, "yo-human: unknown command %q\n\n%s", args[0], usage)
		return ExitUsage
	}
}
