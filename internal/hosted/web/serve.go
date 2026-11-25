package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// The errors Serve returns before it binds anything, and when a shutdown ran out of time.
var (
	ErrMCPNotLoopback = errors.New("web: the MCP listener must be a literal loopback IP address and a port")
	ErrSamePort       = errors.New("web: the two listeners must listen on different ports")
	ErrBadPort        = errors.New("web: the approval address must end in a numeric port")
	ErrForcedClose    = errors.New("web: the shutdown grace ran out and requests in flight were cut")
)

// ServeConfig is where the two listeners bind and how long a shutdown may take.
type ServeConfig struct {
	MCPAddr, ApproveAddr string
	Grace                time.Duration
}

// listener is one server as serve drives it: how it runs, drains and is cut off.
type listener struct {
	name     string
	run      func() error
	shutdown func(context.Context) error
	close    func() error
}

// Serve runs one http.Server per listener, each with the contract's timeouts, until ctx is done or
// one fails. Before it binds anything it repeats the config's two listener checks, so a caller that
// skipped the config still cannot expose MCP. A failure is returned wrapped with the listener's
// name and beats a cancelled ctx. On ctx it drains both within Grace and returns nil, or cuts what
// is left and returns ErrForcedClose.
func Serve(ctx context.Context, h Handlers, c ServeConfig) error {
	if err := checkAddrs(c.MCPAddr, c.ApproveAddr); err != nil {
		return err
	}
	return serve(ctx, []listener{newListener("MCP", c.MCPAddr, h.MCP), newListener("approval", c.ApproveAddr, h.Approve)}, c.Grace)
}

func newListener(name, addr string, h http.Handler) listener {
	s := NewHTTPServer(addr, h)
	return listener{name: name, run: s.ListenAndServe, shutdown: s.Shutdown, close: s.Close}
}

// checkAddrs: the MCP address is a literal loopback IP and a port (IsLoopback unmaps, so Is4In6
// is what refuses ::ffff:127.0.0.1), the approval address ends in a number, and the ports differ.
func checkAddrs(mcp, approve string) error {
	a, err := netip.ParseAddrPort(mcp)
	if err != nil || !a.Addr().IsLoopback() || a.Addr().Is4In6() || a.Addr().Zone() != "" {
		return ErrMCPNotLoopback
	}
	pm, _ := strconv.Atoi(mcp[strings.LastIndex(mcp, ":")+1:]) // ParseAddrPort checked it
	pa, err := strconv.Atoi(approve[strings.LastIndex(approve, ":")+1:])
	switch {
	case err != nil:
		return ErrBadPort
	case pm == pa:
		return ErrSamePort
	}
	return nil
}

type ended struct {
	name string
	err  error
}

// failure is nil for the http.ErrServerClosed of a server serve shut down.
func (o ended) failure() error {
	if errors.Is(o.err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("web: %s listener: %w", o.name, o.err)
}

// serve runs the listeners and returns when they have all ended. The first to end, or ctx, starts
// the shutdown of all; any real failure, from either, is what it returns.
func serve(ctx context.Context, ls []listener, grace time.Duration) error {
	done := make(chan ended, len(ls))
	for _, l := range ls {
		go func() { done <- ended{l.name, l.run()} }()
	}
	var failure error
	pending := len(ls)
	select {
	case o := <-done:
		pending--
		failure = o.failure()
	case <-ctx.Done():
	}
	forced := stop(ls, grace)
	for ; pending > 0; pending-- {
		if o := <-done; failure == nil {
			failure = o.failure()
		}
	}
	switch {
	case failure != nil:
		return failure
	case forced:
		return ErrForcedClose
	}
	return nil
}

// stop shuts every listener down under one grace deadline. When any drain fails, which includes
// running out of time, it closes them all and reports that it had to.
func stop(ls []listener, grace time.Duration) (forced bool) {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	errs := make(chan error, len(ls))
	for _, l := range ls {
		go func() { errs <- l.shutdown(ctx) }()
	}
	for range ls {
		if err := <-errs; err != nil {
			forced = true
		}
	}
	if forced {
		for _, l := range ls {
			_ = l.close()
		}
	}
	return forced
}
