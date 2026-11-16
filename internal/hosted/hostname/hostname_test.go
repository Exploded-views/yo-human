package hostname_test

import (
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/hostname"
)

func TestNormalise(t *testing.T) {
	for in, want := range map[string]string{
		"bus.example.org": "bus.example.org", "BUS.Example.ORG": "bus.example.org", "bus.example.org.": "bus.example.org",
		"bus.example.org:443": "bus.example.org", "Bus.Example.Org.:443": "bus.example.org",
		"bus.example.org:80": "bus.example.org", "BUS.example.org.:80": "bus.example.org",
		"bus.example.org:8443": "bus.example.org:8443", "bus.example.org:8080": "bus.example.org:8080",
		"[::1]:443": "[::1]", "[::1]:80": "[::1]", "[::1]:8443": "[::1]:8443", "[::1]": "[::1]",
	} {
		if got := hostname.Normalise(in); got != want {
			t.Fatalf("Normalise(%q) = %q, want %q", in, got, want)
		}
	}
}
