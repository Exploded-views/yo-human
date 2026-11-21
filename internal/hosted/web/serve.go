package web

import (
	"context"
	"errors"
	"time"
)

// The errors Serve returns before it binds anything, and when a shutdown ran out of time.
var (
	ErrMCPNotLoopback = errors.New("web: the MCP listener must be a literal loopback IP address and a port")
	ErrSamePort       = errors.New("web: the two listeners must listen on different ports")
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

var errNotBuilt = errors.New("web: Serve is not built yet")

// Serve is a stub until its tests are red.
func Serve(_ context.Context, _ Handlers, _ ServeConfig) error { return errNotBuilt }

func serve(_ context.Context, _ []listener, _ time.Duration) error { return errNotBuilt }
