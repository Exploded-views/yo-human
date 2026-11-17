// Package web is hosted mode's inbound HTTP: the two listeners' routing, the MCP handler with its
// middleware, and the tool sets. It never makes an outbound call.
package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/auth"
	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/hostname"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Config is what the listeners and the tools need from the deployment's configuration.
type Config struct {
	PublicURL     string   // YH_PUBLIC_URL: the MCP host's origin
	ApproveOrigin string   // YH_APPROVE_ORIGIN: the approval host's origin, also the base of approval links
	OriginAllow   []string // YH_ORIGIN_ALLOW: the Origin values /mcp accepts
	ToolDeadline  time.Duration
	// Log receives the cause of an internal error, which no result carries. Nil logs nothing.
	Log *slog.Logger
	// Approve serves /approve* and /passkey/* (Tasks 10c and 10d). Nil until they land: the Approve
	// listener then answers 404 for those paths too.
	Approve http.Handler
}

// Handlers is one handler per listener, never mounted on the same one.
type Handlers struct{ MCP, Approve http.Handler }

// service is what both listeners and the MCP pipeline share.
type service struct {
	cfg                  Config
	mcpHost, approveHost string // normalised
	extra                http.Handler
	pipeline             http.Handler
	duet, node, empty    *mcp.Server
}

// New returns the two listeners' handlers. It panics when a configured origin has no host, or both
// have the same one. extra serves the OAuth routes and metadata documents
// (Task 10) on the MCP listener only.
func New(b *bus.Bus, v *auth.Verifier, cfg Config, extra http.Handler) Handlers {
	if cfg.ToolDeadline <= 0 {
		cfg.ToolDeadline = 5 * time.Second
	}
	s := &service{cfg: cfg, mcpHost: hostOf(cfg.PublicURL), approveHost: hostOf(cfg.ApproveOrigin), extra: extra}
	if s.mcpHost == "" || s.approveHost == "" || s.mcpHost == s.approveHost {
		// An empty host would match a request with no Host, and one shared host would put both
		// surfaces on one listener's name. app.LoadConfig checks both; this is the last guard.
		panic("web: the public URL and the approve origin must be two different origins with a host")
	}
	s.duet, s.node, s.empty = s.servers(b)
	s.pipeline = s.mcpPipeline(v, s.sdkHandler())
	return Handlers{MCP: http.HandlerFunc(s.serveMCP), Approve: http.HandlerFunc(s.serveApprove)}
}

func hostOf(origin string) string {
	u, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	return hostname.Normalise(u.Host)
}

// normHost is the one place a request's host is read: r.Host only (for HTTP/2, :authority), never
// a forwarded-host header, through the one shared normaliser.
func normHost(r *http.Request) string { return hostname.Normalise(r.Host) }

// hostIs reports whether r is for want. An empty want or an empty Host matches nothing.
func hostIs(r *http.Request, want string) bool {
	got := normHost(r)
	return want != "" && got != "" && got == want
}

// isApprovePath: the routes that belong to the Approve listener.
func isApprovePath(p string) bool {
	return strings.HasPrefix(p, "/approve") || p == "/passkey" || strings.HasPrefix(p, "/passkey/")
}

// canonical: a path path.Clean would change (a dot segment, a doubled or trailing slash) is
// refused before routing, so no handler behind a listener has to clean one.
func canonical(p string) bool { return path.Clean(p) == p }

// notFound is the one 404 for both listeners, so a probe cannot tell which one it reached.
func notFound(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }

// healthz answers ok under any host, with the same bytes on both listeners.
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok")
}

func isHealthz(r *http.Request) bool { return r.Method == http.MethodGet && r.URL.Path == "/healthz" }

// serveMCP is the MCP listener: POST /mcp and, from extra, the OAuth routes, under the MCP host
// only. The approval routes are never served here.
func (s *service) serveMCP(w http.ResponseWriter, r *http.Request) {
	switch p := r.URL.Path; {
	case !canonical(p):
		notFound(w, r)
	case isHealthz(r):
		healthz(w, r)
	case !hostIs(r, s.mcpHost) || isApprovePath(p):
		notFound(w, r)
	case p == "/mcp":
		s.pipeline.ServeHTTP(w, r)
	case s.extra != nil:
		s.extra.ServeHTTP(w, r)
	default:
		notFound(w, r)
	}
}

// serveApprove is the Approve listener: the approval routes under the approval host only. It has
// no /mcp, no OAuth route and no extra.
func (s *service) serveApprove(w http.ResponseWriter, r *http.Request) {
	switch {
	case !canonical(r.URL.Path):
		notFound(w, r)
	case isHealthz(r):
		healthz(w, r)
	case !hostIs(r, s.approveHost) || !isApprovePath(r.URL.Path) || s.cfg.Approve == nil:
		notFound(w, r)
	default:
		s.cfg.Approve.ServeHTTP(w, r)
	}
}

// NewHTTPServer is an http.Server for one listener with the timeouts the contract fixes. The
// write timeout is 35 s so an answers long-poll of 25 s fits.
func NewHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 35 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
}
