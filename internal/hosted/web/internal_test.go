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
