package app

import (
	"errors"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/hostname"
)

// Secret is a configuration value that must never reach output: every formatting verb prints a
// placeholder, and Reveal is the one way to the value.
type Secret string

// Reveal returns the secret's value.
func (s Secret) Reveal() string { return string(s) }

// String is what %v, %s and %q print.
func (s Secret) String() string { return "[redacted]" }

// GoString is what %#v prints.
func (s Secret) GoString() string { return "app.Secret([redacted])" }

// MarshalText is what JSON and the text encoders, including slog's handlers, write.
func (s Secret) MarshalText() ([]byte, error) { return []byte("[redacted]"), nil }

// Config is yo-human-hosted's configuration, read from the environment. Its secrets are Secret
// values, so printing a Config prints none of them.
type Config struct {
	DBPath, PublicURL                                string
	OwnerGitHubID                                    int64
	GitHubClientID                                   string
	GitHubClientSecret                               Secret
	RedirectAllow, CIMDHosts, OriginAllow, LinkHosts []string
	BarkURL, BarkUser                                string
	BarkPassword, BarkDeviceKey                      Secret
	NudgeWindow                                      time.Duration
	NudgeMinSeverity, NudgeLevel                     string
	EscalateAfter                                    map[string]time.Duration
	WalledSwarms                                     []string
	EscalateCritical                                 bool
	AccessTTL, RefreshTTL, ToolDeadline              time.Duration
	WebAuthnRPID, ApproveOrigin                      string
	ChallengeTTL, DeskTTL                            time.Duration
	ListenMCP, ListenApprove                         string
}

// The ceilings the plan puts on three durations.
const (
	maxChallengeTTL = 10 * time.Minute
	maxDeskTTL      = 60 * time.Minute
	maxEscalate     = 60 * time.Minute
	// maxToolDeadline is the server's 35 s write timeout less the 25 s an answers call may wait:
	// a longer deadline would let the connection be cut under a call that may have committed.
	maxToolDeadline = 10 * time.Second
)

var severities = []string{"low", "normal", "high", "critical"}

// LoadConfig reads the configuration through env and returns the first problem it finds. An
// error names the variable and why, never a value: a value may be a secret.
func LoadConfig(env func(string) string) (Config, error) {
	l := &loader{env: env}
	c := Config{
		DBPath:             dbPath(env, ""),
		PublicURL:          l.origin("YH_PUBLIC_URL", l.required("YH_PUBLIC_URL")),
		OwnerGitHubID:      l.ownerID(),
		GitHubClientID:     l.required("YH_GITHUB_CLIENT_ID"),
		GitHubClientSecret: Secret(l.required("YH_GITHUB_CLIENT_SECRET")),
		RedirectAllow:      l.list("YH_REDIRECT_ALLOW", "https://claude.ai/api/mcp/auth_callback,https://claude.com/api/mcp/auth_callback"),
		CIMDHosts:          l.list("YH_CIMD_HOSTS", "claude.ai,claude.com"),
		OriginAllow:        l.shaped("YH_ORIGIN_ALLOW", l.list("YH_ORIGIN_ALLOW", "https://claude.ai,https://claude.com"), browserOrigin, "an https origin as a browser sends it: scheme and host in lower case, no path, query, fragment, userinfo or :443"),
		LinkHosts:          l.shaped("YH_LINK_HOSTS", l.list("YH_LINK_HOSTS", "github.com"), bareHost, "bare hosts, with no scheme, port, path or wildcard"),
		BarkURL:            l.str("YH_BARK_URL", ""),
		BarkUser:           l.str("YH_BARK_USER", ""),
		BarkPassword:       Secret(l.str("YH_BARK_PASSWORD", "")),
		BarkDeviceKey:      Secret(l.str("YH_BARK_DEVICE_KEY", "")),
		NudgeWindow:        l.dur("YH_NUDGE_WINDOW", "30m", 0),
		NudgeMinSeverity:   l.oneOf("YH_NUDGE_MIN_SEVERITY", "normal", severities...),
		NudgeLevel:         l.oneOf("YH_NUDGE_LEVEL", "active", "active", "passive"),
		EscalateAfter:      l.escalateAfter(),
		EscalateCritical:   l.oneOf("YH_ESCALATE_CRITICAL", "off", "off", "on") == "on",
		AccessTTL:          l.dur("YH_ACCESS_TTL", "1h", 0),
		RefreshTTL:         l.dur("YH_REFRESH_TTL", "720h", 0),
		ToolDeadline:       l.dur("YH_TOOL_DEADLINE", "5s", maxToolDeadline),
		WebAuthnRPID:       l.required("YH_WEBAUTHN_RP_ID"),
		ApproveOrigin:      l.approveOrigin(l.required("YH_APPROVE_ORIGIN")),
		ChallengeTTL:       l.dur("YH_CHALLENGE_TTL", "5m", maxChallengeTTL),
		DeskTTL:            l.dur("YH_DESK_TTL", "15m", maxDeskTTL),
	}
	c.WalledSwarms = l.walled()
	c.ListenMCP, c.ListenApprove = l.listeners()
	l.distinctHosts(c.PublicURL, c.ApproveOrigin)
	l.rpIDMatches(c.WebAuthnRPID, c.ApproveOrigin)
	if l.err != nil {
		return Config{}, l.err
	}
	return c, nil
}

// loader reads variables and keeps the first problem.
type loader struct {
	env func(string) string
	err error
}

func (l *loader) fail(name, why string) {
	if l.err == nil {
		l.err = errors.New(name + ": " + why)
	}
}

func (l *loader) str(name, def string) string {
	if v := l.env(name); v != "" {
		return v
	}
	return def
}

func (l *loader) required(name string) string {
	v := l.env(name)
	if v == "" {
		l.fail(name, "is required")
	}
	return v
}

func (l *loader) ownerID() int64 {
	id, err := strconv.ParseInt(l.required("YH_OWNER_GITHUB_ID"), 10, 64)
	if err != nil || id <= 0 {
		l.fail("YH_OWNER_GITHUB_ID", "must be the owner's numeric GitHub user id")
	}
	return id
}

func (l *loader) oneOf(name, def string, allowed ...string) string {
	v := l.str(name, def)
	if !slices.Contains(allowed, v) {
		l.fail(name, "must be one of "+strings.Join(allowed, ", "))
	}
	return v
}

// duration parses a positive duration no longer than max (when max is set).
func duration(raw string, ceiling time.Duration) (time.Duration, bool) {
	d, err := time.ParseDuration(raw)
	return d, err == nil && d > 0 && (ceiling == 0 || d <= ceiling)
}

func (l *loader) dur(name, def string, ceiling time.Duration) time.Duration {
	d, ok := duration(l.str(name, def), ceiling)
	if !ok {
		why := "must be a positive duration such as 30m"
		if ceiling > 0 {
			why += ", at most " + ceiling.String()
		}
		l.fail(name, why)
	}
	return d
}

// escalateAfter reads YH_ESCALATE_AFTER: every severity, once, each a duration of at most 60m.
func (l *loader) escalateAfter() map[string]time.Duration {
	out := map[string]time.Duration{}
	for _, part := range strings.Split(l.str("YH_ESCALATE_AFTER", "low=30m,normal=30m,high=30m,critical=30m"), ",") {
		sev, raw, _ := strings.Cut(part, "=")
		d, ok := duration(raw, maxEscalate)
		if _, dup := out[sev]; dup || !ok || !slices.Contains(severities, sev) {
			l.fail("YH_ESCALATE_AFTER", "must be low=, normal=, high= and critical= durations, each at most "+maxEscalate.String())
		}
		out[sev] = d
	}
	if len(out) != len(severities) {
		l.fail("YH_ESCALATE_AFTER", "must be low=, normal=, high= and critical= durations, each at most "+maxEscalate.String())
	}
	return out
}

// list splits a comma-separated variable; an empty entry is refused. An unset variable with an
// empty default is nil.
func (l *loader) list(name, def string) []string {
	raw := l.str(name, def)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	for i, p := range parts {
		if parts[i] = strings.TrimSpace(p); parts[i] == "" {
			l.fail(name, "must not have an empty entry")
		}
	}
	return parts
}

// shaped refuses the first entry of a list that ok does not accept. The error names the variable
// and the rule, never the entry.
func (l *loader) shaped(name string, entries []string, ok func(string) bool, rule string) []string {
	for _, e := range entries {
		if !ok(e) {
			l.fail(name, "every entry must be "+rule)
			break
		}
	}
	return entries
}

var hostRE = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*$`)

// bareHost is a DNS name: no scheme, port, path, userinfo or wildcard, at most 253 bytes with
// labels of at most 63, and a last label that is not all digits, which keeps out IP literals in
// any spelling.
func bareHost(s string) bool {
	if len(s) > 253 || !hostRE.MatchString(s) {
		return false
	}
	labels := strings.Split(s, ".")
	for _, l := range labels {
		if len(l) > 63 {
			return false
		}
	}
	return strings.Trim(labels[len(labels)-1], "0123456789") != ""
}

// browserOrigin is an https origin exactly as a browser sends it in an Origin header: the scheme
// and host in lower case, the port only when it is not 443, and nothing after the host (so no
// slash, no empty "?" and no "#"). The match against a request's Origin is byte for byte, so an
// entry that differs in any of these would never match.
func browserOrigin(s string) bool {
	rest, found := strings.CutPrefix(s, "https://")
	if !found || rest != strings.ToLower(rest) {
		return false
	}
	host, port, hasPort := strings.Cut(rest, ":")
	if n, err := strconv.ParseUint(port, 10, 16); hasPort && (err != nil || n == 0 || n == 443 || port != strconv.FormatUint(n, 10)) {
		return false
	}
	return bareHost(host)
}

func (l *loader) walled() []string {
	names := l.list("YH_WALLED_SWARMS", "")
	for _, n := range names {
		if !bus.ValidSwarm(n) {
			l.fail("YH_WALLED_SWARMS", "must be comma-separated swarm names")
		}
	}
	return names
}

// origin checks a scheme-and-host URL with no path, query, fragment or credentials, and drops
// a trailing slash.
func (l *loader) origin(name, raw string) string {
	if raw != "" && !isOrigin(raw) { // an unset variable was already reported as required
		l.fail(name, "must be an http or https origin with a host and no path")
	}
	return strings.TrimSuffix(raw, "/")
}

// approveOrigin is an origin that must be https: it is the one WebAuthn origin.
func (l *loader) approveOrigin(raw string) string {
	if raw != "" && !strings.HasPrefix(raw, "https://") {
		l.fail("YH_APPROVE_ORIGIN", "must be an https origin")
	}
	return l.origin("YH_APPROVE_ORIGIN", raw)
}

// rpIDMatches requires the WebAuthn RP ID to be the approval origin's exact host: bare, lower
// case, no port, and not a parent domain, which would let sibling hosts ask for assertions.
func (l *loader) rpIDMatches(rpID, origin string) {
	if l.err != nil {
		return // the origin is already refused, or unset
	}
	u, _ := url.Parse(origin)
	if rpID != strings.TrimSuffix(strings.ToLower(u.Hostname()), ".") {
		l.fail("YH_WEBAUTHN_RP_ID", "must be the exact host of YH_APPROVE_ORIGIN, lower case, with no port or scheme, never a parent domain")
	}
}

func isOrigin(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && (u.Path == "" || u.Path == "/") &&
		u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

// listeners reads the two listen addresses. The MCP one must be a literal loopback IP and a
// port, because the tunnel connector reaches it from the same machine; the approval one is a
// port, with or without an IP. They must not share a port.
func (l *loader) listeners() (mcp, approve string) {
	mcp = l.str("YH_LISTEN_MCP", "127.0.0.1:8081")
	ap, err := netip.ParseAddrPort(mcp)
	a := ap.Addr()
	if err != nil || ap.Port() == 0 || !a.IsLoopback() || a.Is4In6() || a.Zone() != "" {
		l.fail("YH_LISTEN_MCP", "must be a literal loopback IP address and a port, such as 127.0.0.1:8081")
	}
	approve = l.str("YH_LISTEN_APPROVE", ":8080")
	port, ok := listenPort(approve)
	if !ok {
		l.fail("YH_LISTEN_APPROVE", "must be a port, optionally after a literal IP address")
	}
	if ok && port == ap.Port() {
		l.fail("YH_LISTEN_MCP", "and YH_LISTEN_APPROVE must listen on different ports")
	}
	return mcp, approve
}

// listenPort returns the port of ":port" or "ip:port", where ip is a literal address.
func listenPort(addr string) (uint16, bool) {
	if rest, ok := strings.CutPrefix(addr, ":"); ok {
		p, err := strconv.ParseUint(rest, 10, 16)
		return uint16(p), err == nil && p != 0
	}
	ap, err := netip.ParseAddrPort(addr)
	return ap.Port(), err == nil && ap.Port() != 0
}

// distinctHosts refuses a public URL and an approval origin on the same host, so the approval
// page and the MCP surface never share an origin.
func (l *loader) distinctHosts(public, approve string) {
	pu, _ := url.Parse(public)
	au, _ := url.Parse(approve)
	if l.err == nil && hostname.Normalise(pu.Host) == hostname.Normalise(au.Host) {
		l.fail("YH_PUBLIC_URL", "and YH_APPROVE_ORIGIN must be on different hosts")
	}
}

// dbPath is the database path: the --db flag, else YH_DB_PATH, else /data/bus.db.
func dbPath(env func(string) string, flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := env("YH_DB_PATH"); v != "" {
		return v
	}
	return "/data/bus.db"
}
