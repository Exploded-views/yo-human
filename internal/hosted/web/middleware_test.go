package web_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/web"
)

func TestNoTokenGets401WithResourceMetadata(t *testing.T) {
	h := newHarness(t)
	want := `Bearer resource_metadata="https://bus.example.com/.well-known/oauth-protected-resource/mcp"`
	var first string
	for name, tok := range map[string]string{"no token": "", "an unknown token": h.node + "x", "a malformed token": "not-a-token"} {
		resp, body := h.do(h.h.MCP, mcpPost(tok))
		if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("WWW-Authenticate") != want {
			t.Errorf("%s: %d, WWW-Authenticate %q, want 401 and %q", name, resp.StatusCode, resp.Header.Get("WWW-Authenticate"), want)
		}
		if name != "no token" && first == "" {
			first = body
		} else if name != "no token" && body != first {
			t.Errorf("%s: body %q differs from another invalid token's %q", name, body, first)
		}
	}
}

func TestDeskStarterGets403OnMCP(t *testing.T) {
	h := newHarness(t)
	resp, _ := h.do(h.h.MCP, mcpPost(h.starter))
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a desk starter: %d, want 403", resp.StatusCode)
	}
	for name, tok := range map[string]string{"a node": h.node, "Duet": h.duet} {
		if resp, _ := h.do(h.h.MCP, mcpPost(tok)); resp.StatusCode != http.StatusOK {
			t.Errorf("%s: %d, want 200", name, resp.StatusCode)
		}
	}
}

func TestBadOriginGets403(t *testing.T) {
	h := newHarness(t)
	for _, c := range []struct {
		origin, token string
		want          int
	}{
		{"https://evil.example", h.node, 403},
		{"https://evil.example", "", 403}, // the Origin check comes before the bearer check
		{"https://claude.ai/", h.node, 403},
		{"http://claude.ai", h.node, 403},
		{"https://CLAUDE.ai", h.node, 403},
		{"https://claude.ai", h.node, 200},
		{"", h.node, 200},
		{"https://claude.ai", "", 401},
	} {
		r := mcpPost(c.token)
		if c.origin != "" {
			r.hdr["Origin"] = c.origin
		}
		if resp, _ := h.do(h.h.MCP, r); resp.StatusCode != c.want {
			t.Errorf("Origin %q, token %v: %d, want %d", c.origin, c.token != "", resp.StatusCode, c.want)
		}
	}
}

func TestBodyOver64KiBRejected(t *testing.T) {
	h := newHarness(t)
	r := mcpPost(h.node)
	r.body = `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":"` + strings.Repeat("a", 64<<10) + `"}}`
	if resp, _ := h.do(h.h.MCP, r); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("a body over 64 KiB: %d, want 413", resp.StatusCode)
	}
	// Without a Content-Length the reader's own limit applies.
	r = mcpPost(h.node)
	r.body = `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":"` + strings.Repeat("a", 64<<10) + `"}}`
	r.hdr["Transfer-Encoding"] = "chunked"
	if resp, _ := h.do(h.h.MCP, r); resp.StatusCode < 400 {
		t.Errorf("a chunked body over 64 KiB: %d, want an error status", resp.StatusCode)
	}
}

func TestHTTPServerTimeouts(t *testing.T) {
	s := web.NewHTTPServer("127.0.0.1:1", http.NotFoundHandler())
	got := []any{s.Addr, s.ReadHeaderTimeout, s.ReadTimeout, s.WriteTimeout, s.IdleTimeout, s.MaxHeaderBytes}
	want := []any{"127.0.0.1:1", 5 * time.Second, 10 * time.Second, 35 * time.Second, 60 * time.Second, 16 << 10}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("server setting %d = %v, want %v", i, got[i], want[i])
		}
	}
}
