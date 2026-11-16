// Package hostname normalises a host or host:port so two spellings of one host compare equal.
// It is standard library only, below both the config check in app and the listeners in the web
// layer.
package hostname

import "strings"

// Normalise lowercases hostport, drops a trailing dot on the host and drops a default :443 or
// :80. Task 9's web layer must call this from its own normHost, never carry a second copy: the
// config's same-host check and the listeners' routing check must agree.
func Normalise(hostport string) string {
	h := strings.ToLower(hostport)
	host, port := h, ""
	if i := strings.LastIndex(h, ":"); i >= 0 && !strings.Contains(h[i:], "]") {
		host, port = h[:i], h[i+1:]
	}
	host = strings.TrimSuffix(host, ".")
	if port == "" || port == "443" || port == "80" {
		return host
	}
	return host + ":" + port
}
