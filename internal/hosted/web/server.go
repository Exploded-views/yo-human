// Package web is hosted mode's inbound HTTP: the MCP handler and its tools, and the two listeners'
// routing. It never makes an outbound call.
package web

import (
	"net/http"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/auth"
	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// Config is what the listeners and the tools need from the deployment's configuration.
type Config struct {
	PublicURL, ApproveOrigin string
	OriginAllow              []string
	ToolDeadline             time.Duration
	Approve                  http.Handler
}

// Handlers is one handler per listener, never mounted on the same one.
type Handlers struct{ MCP, Approve http.Handler }

// New is a stub until the fix commit.
func New(b *bus.Bus, v *auth.Verifier, cfg Config, extra http.Handler) Handlers {
	stub := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "stub", http.StatusInternalServerError) })
	return Handlers{MCP: stub, Approve: stub}
}

// NewHTTPServer is a stub until the fix commit.
func NewHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: time.Second}
}
