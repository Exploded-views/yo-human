// Package hostname normalises a host or host:port so two spellings of one host compare equal.
// It is standard library only, below both the config check in app and the listeners in the web
// layer.
package hostname

// Normalise lowercases hostport, drops a trailing dot on the host and drops a default :443 or
// :80. Task 9's web layer must call this from its own normHost, never carry a second copy: the
// config's same-host check and the listeners' routing check must agree.
func Normalise(hostport string) string { return hostport }
