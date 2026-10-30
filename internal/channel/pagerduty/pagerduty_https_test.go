package pagerduty

import (
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
)

// The events URL carries the routing key in every body: it must be https, with a host and no
// credentials. Plain http is refused for loopback too; a test server is httptest's TLS server, and
// the e2e build trusts its certificate through the test-only CA hook, never by allowing http.
func TestPagerDutyEventsURLMustBeHTTPS(t *testing.T) {
	refused := []string{
		"http://events.example/v2/enqueue", "http://127.0.0.1:8080/v2/enqueue", "http://localhost/v2", "ftp://events.example/v2",
		"https://u:p" + "@events.example/v2", "https://user" + "@events.example/v2", "https://", "https://:443/v2", "", "events.example/v2", // split in the source so the public-repo scrub stays uniform
	}
	for _, s := range refused {
		_, err := NewType("test").New(channel.Instance{Name: "n", Settings: map[string]any{"events_url": s}, Secrets: map[string]string{"routing_key": fakeKey}})
		if err == nil || channel.KindOf(err) != channel.ConfigError {
			t.Errorf("events_url %q: err=%v; want a config error", s, err)
			continue
		}
		if msg := err.Error(); !strings.Contains(msg, "https") || (s != "" && strings.Contains(msg, s)) {
			t.Errorf("events_url %q: error %q; want it to ask for https without echoing the value", s, msg)
		}
	}
	for _, s := range []string{"https://events.example/v2/enqueue", "https://127.0.0.1:8443/v2/enqueue"} {
		if _, err := NewType("test").New(channel.Instance{Settings: map[string]any{"events_url": s}, Secrets: map[string]string{"routing_key": fakeKey}}); err != nil {
			t.Errorf("events_url %q: %v; want it accepted", s, err)
		}
	}
}
