package app

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/auth"
	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
	"github.com/Exploded-views/yo-human/internal/hosted/web"
)

// serveGrace is how long a shutdown may drain before it cuts what is left. fly.toml's
// kill_timeout is longer.
const serveGrace = 10 * time.Second

// serveFn and notifyContext are the two seams a test replaces: the listeners, and the signal
// context.
var (
	serveFn       = web.Serve
	notifyContext = func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	}
)

// runServe is `serve`: it loads the configuration, opens the database, builds the bus, the
// verifier and the two handlers, and serves them until SIGTERM or SIGINT.
func runServe(args []string, env func(string) string, stdout, stderr io.Writer) int {
	a := admin{name: "serve", env: env, stdout: stdout, stderr: stderr}
	if code, stop := a.parse(flag.NewFlagSet(a.name, flag.ContinueOnError), args); stop {
		return code
	}
	cfg, err := LoadConfig(env)
	if err != nil {
		a.say("serve: %v", err)
		return ExitConfig
	}
	d, err := db.Open(cfg.DBPath)
	if err != nil {
		a.say("serve: %v", err)
		return ExitStore
	}
	defer func() { _ = d.Close() }()
	// OAuth (Task 10), the approval page and policy file (Tasks 10c, 10d, 10f) and the nudge worker
	// (Task 11) join here as they land. Until then Duet cannot connect, no approval is on the
	// policy, and nothing is sent.
	h := web.New(bus.New(d, now, bus.Limits{LinkHosts: cfg.LinkHosts, WalledSwarms: cfg.WalledSwarms}),
		auth.NewVerifier(d, now, cfg.DeskTTL),
		web.Config{PublicURL: cfg.PublicURL, ApproveOrigin: cfg.ApproveOrigin, OriginAllow: cfg.OriginAllow,
			ToolDeadline: cfg.ToolDeadline, Log: slog.New(slog.NewTextHandler(stderr, nil))}, nil)
	ctx, stop := notifyContext()
	defer stop()
	// The first signal ends ctx; restore the default handling then, so a second one kills the drain.
	context.AfterFunc(ctx, stop)
	return a.served(serveFn(ctx, h, web.ServeConfig{MCPAddr: cfg.ListenMCP, ApproveAddr: cfg.ListenApprove, Grace: serveGrace}))
}

// served maps what Serve returned to an exit code. A forced close is the deploy cutting a
// long-poll, which is expected; a refused address is configuration; anything else is internal.
func (a admin) served(err error) int {
	switch {
	case err == nil || errors.Is(err, web.ErrForcedClose):
		return ExitOK
	case errors.Is(err, web.ErrMCPNotLoopback) || errors.Is(err, web.ErrSamePort) || errors.Is(err, web.ErrBadPort):
		a.say("serve: %v", err)
		return ExitConfig
	}
	a.say("serve: %v", err)
	return ExitInternal
}
