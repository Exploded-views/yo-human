package app

import (
	"errors"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
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
		OriginAllow:        l.list("YH_ORIGIN_ALLOW", "https://claude.ai,https://claude.com"),
		LinkHosts:          l.list("YH_LINK_HOSTS", "github.com"),
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
		ToolDeadline:       l.dur("YH_TOOL_DEADLINE", "5s", 0),
		WebAuthnRPID:       l.required("YH_WEBAUTHN_RP_ID"),
		ApproveOrigin:      l.origin("YH_APPROVE_ORIGIN", l.required("YH_APPROVE_ORIGIN")),
		ChallengeTTL:       l.dur("YH_CHALLENGE_TTL", "5m", maxChallengeTTL),
		DeskTTL:            l.dur("YH_DESK_TTL", "15m", maxDeskTTL),
	}
	c.WalledSwarms = l.walled()
	c.ListenMCP, c.ListenApprove = l.listeners()
	l.distinctHosts(c.PublicURL, c.ApproveOrigin)
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
	if l.err == nil && NormaliseHost(pu.Host) == NormaliseHost(au.Host) {
		l.fail("YH_PUBLIC_URL", "and YH_APPROVE_ORIGIN must be on different hosts")
	}
}

// NormaliseHost puts a host or host:port in a form where two spellings of one host compare
// equal: lower case, no trailing dot, no default :443. Task 9 reuses it for its origin checks.
func NormaliseHost(hostport string) string {
	h := strings.ToLower(hostport)
	host, port := h, ""
	if i := strings.LastIndex(h, ":"); i >= 0 && !strings.Contains(h[i:], "]") {
		host, port = h[:i], h[i+1:]
	}
	host = strings.TrimSuffix(host, ".")
	if port == "" || port == "443" {
		return host
	}
	return host + ":" + port
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
