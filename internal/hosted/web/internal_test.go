package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/auth"
	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

func TestPrincipalOfNothingIsTheZeroPrincipal(t *testing.T) {
	if p := principalOf(nil); p != (bus.Principal{}) {
		t.Errorf("principalOf(nil) = %+v, want the zero Principal", p)
	}
}

func TestHostOfBadOriginMatchesNothing(t *testing.T) {
	if got := hostOf("://not a url"); got != "" {
		t.Errorf("hostOf(bad) = %q, want empty, which matches no host", got)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "bus.example.com"
	if hostIs(r, "") {
		t.Error("an empty configured host matched")
	}
	r.Host = ""
	if hostIs(r, "bus.example.com") {
		t.Error("an empty Host matched")
	}
}

// TestNewDefaultsAndNoExtra: a zero tool deadline becomes 5 s, and with no extra handler every
// path but /mcp and /healthz is 404 under the MCP host.
func TestNewDefaultsAndNoExtra(t *testing.T) {
	h := New(bus.New(nil, time.Now, bus.Limits{}), auth.NewVerifier(nil, time.Now, time.Minute),
		Config{PublicURL: "https://bus.example.com", ApproveOrigin: "https://approve.example.com"}, nil)
	for _, p := range []string{"/oauth/github/callback", "/.well-known/oauth-protected-resource/mcp"} {
		r := httptest.NewRequest(http.MethodGet, p, nil)
		r.Host = "bus.example.com"
		w := httptest.NewRecorder()
		h.MCP.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s with no extra handler: %d, want 404", p, w.Code)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/approve/x", nil)
	r.Host = "approve.example.com"
	w := httptest.NewRecorder()
	h.Approve.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("/approve/x with no approval handler: %d, want 404", w.Code)
	}
}

// TestDeadlineMiddlewareBoundsTheWholeRequest: the context a request reaches the MCP handler with
// ends at the tool deadline plus the longest wait a call may ask for, which also bounds a JSON-RPC
// batch of calls.
func TestDeadlineMiddlewareBoundsTheWholeRequest(t *testing.T) {
	s := &service{cfg: Config{ToolDeadline: 3 * time.Second}}
	var left time.Duration
	var has bool
	h := s.deadline(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		d, ok := r.Context().Deadline()
		left, has = time.Until(d), ok
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if want := 3*time.Second + bus.MaxWait; !has || left > want || left < want-2*time.Second {
		t.Errorf("deadline in %v (set %v), want about %v", left, has, want)
	}
}

// TestHostIsFailsClosedOnEmptyHosts: an empty Host never matches, even an empty configured host.
func TestHostIsFailsClosedOnEmptyHosts(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = ""
	if hostIs(r, "") {
		t.Error("an empty Host matched an empty configured host")
	}
}

// TestNewRefusesAnUnusableOrigin: a public or approve origin with no host would match a request
// with no host, so New refuses to start with one.
func TestNewRefusesAnUnusableOrigin(t *testing.T) {
	for _, c := range []Config{
		{PublicURL: "", ApproveOrigin: "https://approve.example.com"},
		{PublicURL: "https://bus.example.com", ApproveOrigin: ""},
		{PublicURL: "://bad", ApproveOrigin: "https://approve.example.com"},
		{PublicURL: "https://bus.example.com", ApproveOrigin: "https:///path"},
		{PublicURL: "https://bus.example.com", ApproveOrigin: "https://bus.example.com"},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("New(%+v) did not refuse", c)
				}
			}()
			New(bus.New(nil, time.Now, bus.Limits{}), auth.NewVerifier(nil, time.Now, time.Minute), c, nil)
		}()
	}
}
