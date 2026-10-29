// Command cmd is the hosted perf helper that tools/perf runs: `go run ./internal/hosted/perf/cmd
// post|revise N` prints N timings, one per line. It is not part of any release.
package main

import (
	"os"

	"github.com/Exploded-views/yo-human/internal/hosted/perf"
)

// exit is os.Exit, a variable so a test can run main.
var exit = os.Exit

func main() {}

var _ = perf.Run
