// Command yo-human is the one way a crew of AI agents reaches their human.
package main

import (
	"os"
	_ "time/tzdata" // the static binary may run where there is no zoneinfo; quiet_hours.timezone must still load

	"github.com/Exploded-views/yo-human/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
