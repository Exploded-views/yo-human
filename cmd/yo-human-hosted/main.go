// Command yo-human-hosted is hosted mode: the always-on bus between a crew and its human.
package main

import (
	"os"

	"github.com/Exploded-views/yo-human/internal/hosted/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}
