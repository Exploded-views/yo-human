package bark

import (
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
)

// The Bark server carries the device key in every request path: it must be https, with a host and
// no credentials. Plain http is refused for loopback too; a test server is an https one (httptest's
// TLS server), and the e2e build trusts its certificate through the test-only CA hook, never by
// allowing http.
func TestBarkServerMustBeHTTPS(t *testing.T) {
	refused := []string{
		"http://bark.example", "http://127.0.0.1:8080", "http://localhost", "ftp://bark.example",
		"https://u:p" + "@bark.example", "https://user" + "@bark.example", "https://", "https://:443", "", "bark.example", // split in the source so the public-repo scrub stays uniform
	}
	for _, s := range refused {
		_, err := NewType("test").New(channel.Instance{Name: "n", Settings: map[string]any{"server": s}, Secrets: map[string]string{"device_key": fakeKey}})
		if err == nil || channel.KindOf(err) != channel.ConfigError {
			t.Errorf("server %q: err=%v; want a config error", s, err)
			continue
		}
		if msg := err.Error(); !strings.Contains(msg, "https") || (s != "" && strings.Contains(msg, s)) {
			t.Errorf("server %q: error %q; want it to ask for https without echoing the value", s, msg)
		}
	}
	for _, s := range []string{"https://bark.example", "https://bark.example/", "https://127.0.0.1:8443"} {
		if _, err := NewType("test").New(channel.Instance{Settings: map[string]any{"server": s}, Secrets: map[string]string{"device_key": fakeKey}}); err != nil {
			t.Errorf("server %q: %v; want it accepted", s, err)
		}
	}
}
