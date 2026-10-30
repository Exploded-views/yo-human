package channel

import (
	"net/url"
	"unicode/utf8"
)

// The helpers below are shared by every channel implementation, which may import only the
// standard library, this package and internal/channel/transport, so none of them is copied per
// provider. The helpers that touch the network are in transport: this package imports no network
// package (constitution IV, single network path).

// Truncate cuts s to at most max bytes at a rune boundary, ending with marker so the cut is
// visible. A max too small for the marker cuts without one.
func Truncate(s string, max int, marker string) string {
	if len(s) <= max {
		return s
	}
	cut := max - len(marker)
	if cut < 0 {
		cut, marker = max, ""
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + marker
}

// SafeLink reports whether s is a plain http(s) URL with a host and no credentials: the only
// kind a channel may hand to a phone, which would open an intent: or custom scheme, and where
// userinfo disguises the real host.
func SafeLink(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil
}
