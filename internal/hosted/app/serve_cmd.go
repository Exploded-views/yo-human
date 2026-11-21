package app

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/Exploded-views/yo-human/internal/hosted/web"
)

// serveFn and notifyContext are the two seams a test replaces: the listeners, and the signal
// context.
var (
	serveFn       = web.Serve
	notifyContext = func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	}
)

// runServe is a stub until its tests are red.
func runServe(_ []string, _ func(string) string, _, _ io.Writer) int { return ExitInternal }
