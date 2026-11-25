package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"
)

// required is the smallest environment LoadConfig accepts. The secret is a fixture.
func required() map[string]string {
	return map[string]string{
		"YH_PUBLIC_URL":           "https://bus.example.org",
		"YH_OWNER_GITHUB_ID":      "12345",
		"YH_GITHUB_CLIENT_ID":     "client-id-FAKE",
		"YH_GITHUB_CLIENT_SECRET": "GITHUB-SECRET-FAKE",
		"YH_WEBAUTHN_RP_ID":       "approve.example.org",
		"YH_APPROVE_ORIGIN":       "https://approve.example.org",
	}
}

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func with(kv ...string) map[string]string {
	m := required()
	for i := 0; i < len(kv); i += 2 {
		if kv[i+1] == "" {
			delete(m, kv[i])
		} else {
			m[kv[i]] = kv[i+1]
		}
	}
	return m
}

func TestConfigMissingRequired(t *testing.T) {
	for k := range required() {
		_, err := LoadConfig(envOf(with(k, "")))
		if err == nil || !strings.Contains(err.Error(), k) {
			t.Fatalf("without %s: err=%v, want it named", k, err)
		}
		if strings.Contains(err.Error(), "GITHUB-SECRET-FAKE") {
			t.Fatalf("the error for %s prints the secret", k)
		}
	}
	// A bad value for the secret's neighbours never prints it either.
	_, err := LoadConfig(envOf(with("YH_OWNER_GITHUB_ID", "not-a-number")))
	if err == nil || !strings.Contains(err.Error(), "YH_OWNER_GITHUB_ID") || strings.Contains(err.Error(), "GITHUB-SECRET-FAKE") {
		t.Fatalf("err=%v", err)
	}
}

type field struct {
	name      string
	got, want any
}

func checkFields(t *testing.T, fs []field) {
	t.Helper()
	for _, f := range fs {
		if !reflect.DeepEqual(f.got, f.want) {
			t.Fatalf("%s = %v, want %v", f.name, f.got, f.want)
		}
	}
}

func load(t *testing.T, m map[string]string) Config {
	t.Helper()
	c, err := LoadConfig(envOf(m))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestConfigDefaults(t *testing.T) {
	c := load(t, required())
	thirty := 30 * time.Minute
	checkFields(t, []field{
		{"DBPath", c.DBPath, "/data/bus.db"}, {"PublicURL", c.PublicURL, "https://bus.example.org"},
		{"OwnerGitHubID", c.OwnerGitHubID, int64(12345)}, {"GitHubClientID", c.GitHubClientID, "client-id-FAKE"},
		{"GitHubClientSecret", c.GitHubClientSecret.Reveal(), "GITHUB-SECRET-FAKE"},
		{"WebAuthnRPID", c.WebAuthnRPID, "approve.example.org"}, {"ApproveOrigin", c.ApproveOrigin, "https://approve.example.org"},
		{"RedirectAllow", c.RedirectAllow, []string{"https://claude.ai/api/mcp/auth_callback", "https://claude.com/api/mcp/auth_callback"}},
		{"CIMDHosts", c.CIMDHosts, []string{"claude.ai", "claude.com"}},
		{"OriginAllow", c.OriginAllow, []string{"https://claude.ai", "https://claude.com"}},
		{"LinkHosts", c.LinkHosts, []string{"github.com"}}, {"WalledSwarms", c.WalledSwarms, []string(nil)},
		{"NudgeWindow", c.NudgeWindow, thirty}, {"AccessTTL", c.AccessTTL, time.Hour},
		{"RefreshTTL", c.RefreshTTL, 720 * time.Hour}, {"ToolDeadline", c.ToolDeadline, 5 * time.Second},
		{"ChallengeTTL", c.ChallengeTTL, 5 * time.Minute}, {"DeskTTL", c.DeskTTL, 15 * time.Minute},
		{"EscalateAfter", c.EscalateAfter, map[string]time.Duration{"low": thirty, "normal": thirty, "high": thirty, "critical": thirty}},
		{"NudgeMinSeverity", c.NudgeMinSeverity, "normal"}, {"NudgeLevel", c.NudgeLevel, "active"},
		{"EscalateCritical", c.EscalateCritical, false}, {"ListenMCP", c.ListenMCP, "127.0.0.1:8081"}, {"ListenApprove", c.ListenApprove, ":8080"},
	})
}

func TestConfigBarkHasNoDefault(t *testing.T) {
	c := load(t, required())
	checkFields(t, []field{
		{"BarkURL", c.BarkURL, ""}, {"BarkUser", c.BarkUser, ""},
		{"BarkPassword", c.BarkPassword.Reveal(), ""}, {"BarkDeviceKey", c.BarkDeviceKey.Reveal(), ""},
	})
}

func TestConfigOverrides(t *testing.T) {
	c := load(t, with(
		"YH_DB_PATH", "/var/bus.db", "YH_NUDGE_WINDOW", "10m", "YH_NUDGE_MIN_SEVERITY", "high", "YH_NUDGE_LEVEL", "passive",
		"YH_ESCALATE_AFTER", "low=45m,normal=40m,high=30m,critical=20m", "YH_ESCALATE_CRITICAL", "on",
		"YH_WALLED_SWARMS", "client, other", "YH_LINK_HOSTS", "github.com,example.org", "YH_CHALLENGE_TTL", "10m",
		"YH_DESK_TTL", "1h", "YH_LISTEN_MCP", "[::1]:9000", "YH_LISTEN_APPROVE", "0.0.0.0:9001",
		"YH_BARK_URL", "https://bark.example.org", "YH_BARK_USER", "BARK-USER-FAKE", "YH_BARK_PASSWORD", "BARK-PASSWORD-FAKE", "YH_BARK_DEVICE_KEY", "DEVICE-KEY-FAKE",
		"YH_ACCESS_TTL", "2h", "YH_REFRESH_TTL", "48h", "YH_TOOL_DEADLINE", "2s",
		"YH_REDIRECT_ALLOW", "https://a.example.org/cb", "YH_CIMD_HOSTS", "a.example.org", "YH_ORIGIN_ALLOW", "https://a.example.org",
	))
	checkFields(t, []field{
		{"DBPath", c.DBPath, "/var/bus.db"}, {"NudgeWindow", c.NudgeWindow, 10 * time.Minute},
		{"NudgeMinSeverity", c.NudgeMinSeverity, "high"}, {"NudgeLevel", c.NudgeLevel, "passive"},
		{"EscalateAfter", c.EscalateAfter, map[string]time.Duration{"low": 45 * time.Minute, "normal": 40 * time.Minute, "high": 30 * time.Minute, "critical": 20 * time.Minute}},
		{"EscalateCritical", c.EscalateCritical, true}, {"WalledSwarms", c.WalledSwarms, []string{"client", "other"}},
		{"LinkHosts", c.LinkHosts, []string{"github.com", "example.org"}}, {"ChallengeTTL", c.ChallengeTTL, 10 * time.Minute},
		{"DeskTTL", c.DeskTTL, time.Hour}, {"ListenMCP", c.ListenMCP, "[::1]:9000"}, {"ListenApprove", c.ListenApprove, "0.0.0.0:9001"},
		{"BarkURL", c.BarkURL, "https://bark.example.org"}, {"BarkUser", c.BarkUser, "BARK-USER-FAKE"},
		{"BarkPassword", c.BarkPassword.Reveal(), "BARK-PASSWORD-FAKE"}, {"BarkDeviceKey", c.BarkDeviceKey.Reveal(), "DEVICE-KEY-FAKE"},
		{"AccessTTL", c.AccessTTL, 2 * time.Hour}, {"RefreshTTL", c.RefreshTTL, 48 * time.Hour}, {"ToolDeadline", c.ToolDeadline, 2 * time.Second},
		{"RedirectAllow", c.RedirectAllow, []string{"https://a.example.org/cb"}}, {"CIMDHosts", c.CIMDHosts, []string{"a.example.org"}},
		{"OriginAllow", c.OriginAllow, []string{"https://a.example.org"}},
	})
}

func TestConfigRefusesBadValues(t *testing.T) {
	cases := []struct{ name, key, val string }{
		{"a non-duration", "YH_NUDGE_WINDOW", "soon"},
		{"a zero duration", "YH_ACCESS_TTL", "0s"},
		{"a negative duration", "YH_TOOL_DEADLINE", "-5s"},
		{"challenge over 10m", "YH_CHALLENGE_TTL", "11m"},
		{"desk over 60m", "YH_DESK_TTL", "61m"},
		{"escalate over 60m", "YH_ESCALATE_AFTER", "low=61m,normal=30m,high=30m,critical=30m"},
		{"escalate missing a severity", "YH_ESCALATE_AFTER", "low=30m,normal=30m,high=30m"},
		{"escalate unknown severity", "YH_ESCALATE_AFTER", "low=30m,normal=30m,high=30m,critical=30m,urgent=1m"},
		{"escalate no duration", "YH_ESCALATE_AFTER", "low,normal=30m,high=30m,critical=30m"},
		{"escalate bad duration", "YH_ESCALATE_AFTER", "low=x,normal=30m,high=30m,critical=30m"},
		{"escalate zero", "YH_ESCALATE_AFTER", "low=0s,normal=30m,high=30m,critical=30m"},
		{"unknown nudge level", "YH_NUDGE_LEVEL", "loud"},
		{"unknown severity", "YH_NUDGE_MIN_SEVERITY", "urgent"},
		{"critical switch", "YH_ESCALATE_CRITICAL", "yes"},
		{"a walled name that is no name", "YH_WALLED_SWARMS", "client,Not A Name"},
		{"an owner id of zero", "YH_OWNER_GITHUB_ID", "0"},
		{"a relative public url", "YH_PUBLIC_URL", "bus.example.org"},
		{"a public url with a path", "YH_PUBLIC_URL", "https://bus.example.org/x"},
		{"an unparsable public url", "YH_PUBLIC_URL", "https://bus%zz.example.org"},
		{"a non-http public url", "YH_PUBLIC_URL", "ftp://bus.example.org"},
		{"an approve origin with a path", "YH_APPROVE_ORIGIN", "https://approve.example.org/x"},
		{"a bare approve origin", "YH_APPROVE_ORIGIN", "approve.example.org"},
		{"a bad listen address", "YH_LISTEN_APPROVE", "8080"},
		{"an empty list entry", "YH_LINK_HOSTS", "github.com,,example.org"},
	}
	for _, c := range cases {
		_, err := LoadConfig(envOf(with(c.key, c.val)))
		if err == nil || !strings.Contains(err.Error(), c.key) {
			t.Fatalf("%s: err=%v, want %s named", c.name, err, c.key)
		}
		if strings.Contains(err.Error(), "GITHUB-SECRET-FAKE") || (c.val != "" && len(c.val) > 3 && strings.Contains(err.Error(), c.val)) {
			t.Fatalf("%s: the error repeats a value: %v", c.name, err)
		}
	}
}

func TestConfigRefusesPublicMCPListen(t *testing.T) {
	for _, bad := range []string{":8081", "0.0.0.0:8081", "[::]:8081", "localhost:8081", "[::ffff:127.0.0.1]:8081", "10.0.0.5:8081", "192.168.1.2:8081", "[2001:db8::1]:8081", "127.0.0.1", "127.0.0.1:0", "127.0.0.1:http", "[::1%lo0]:8081"} {
		m := with("YH_LISTEN_MCP", bad, "YH_LISTEN_APPROVE", ":9090")
		_, err := LoadConfig(envOf(m))
		if err == nil || !strings.Contains(err.Error(), "YH_LISTEN_MCP") {
			t.Fatalf("YH_LISTEN_MCP=%q: err=%v, want it refused and named", bad, err)
		}
	}
	for _, ok := range []string{"127.0.0.1:8081", "[::1]:8081", "127.0.0.2:8081"} {
		if _, err := LoadConfig(envOf(with("YH_LISTEN_MCP", ok))); err != nil {
			t.Fatalf("YH_LISTEN_MCP=%q: %v", ok, err)
		}
	}
}

func TestConfigRefusesSamePort(t *testing.T) {
	for _, c := range [][2]string{{"127.0.0.1:8080", ":8080"}, {"127.0.0.1:8080", "0.0.0.0:8080"}, {"[::1]:9000", "[::]:9000"}} {
		_, err := LoadConfig(envOf(with("YH_LISTEN_MCP", c[0], "YH_LISTEN_APPROVE", c[1])))
		if err == nil || !strings.Contains(err.Error(), "YH_LISTEN_MCP") || !strings.Contains(err.Error(), "YH_LISTEN_APPROVE") {
			t.Fatalf("%v: err=%v, want both variables named", c, err)
		}
	}
}

func TestConfigRefusesSameHost(t *testing.T) {
	for _, c := range []struct{ public, approve string }{
		{"https://bus.example.org", "https://bus.example.org"},
		{"https://bus.example.org", "https://BUS.Example.ORG"},
		{"https://bus.example.org", "https://bus.example.org."},
		{"https://bus.example.org", "https://bus.example.org:443"},
		{"https://bus.example.org:443", "https://bus.example.org"},
		{"https://Bus.Example.Org.:443", "https://bus.example.org"},
	} {
		_, err := LoadConfig(envOf(with("YH_PUBLIC_URL", c.public, "YH_APPROVE_ORIGIN", c.approve, "YH_WEBAUTHN_RP_ID", "bus.example.org")))
		if err == nil || !strings.Contains(err.Error(), "YH_PUBLIC_URL") || !strings.Contains(err.Error(), "YH_APPROVE_ORIGIN") {
			t.Fatalf("%+v: err=%v, want both variables named", c, err)
		}
	}
	if _, err := LoadConfig(envOf(with("YH_PUBLIC_URL", "https://bus.example.org:8443"))); err != nil {
		t.Fatalf("a different port is a different host: %v", err)
	}
}

func TestConfigNeverPrintsSecrets(t *testing.T) {
	c, err := LoadConfig(envOf(with("YH_BARK_PASSWORD", "BARK-PASSWORD-FAKE", "YH_BARK_DEVICE_KEY", "DEVICE-KEY-FAKE")))
	if err != nil {
		t.Fatal(err)
	}
	str := func(verb string, v any) string { return fmt.Sprintf(verb, v) }
	var js, sl, tx bytes.Buffer
	enc, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	js.Write(enc)
	enc, _ = json.Marshal(c.BarkPassword)
	js.Write(enc)
	slog.New(slog.NewJSONHandler(&sl, nil)).Info("config", "cfg", c, "secret", c.GitHubClientSecret)
	slog.New(slog.NewTextHandler(&tx, nil)).Info("config", "cfg", c, "secret", c.GitHubClientSecret)
	for _, s := range []string{js.String(), sl.String(), tx.String(), str("%v", c), str("%+v", c), str("%#v", c), str("%v", c.GitHubClientSecret), str("%q", c.BarkPassword), str("%s", c.BarkDeviceKey), str("%x", c.BarkDeviceKey)} {
		for _, secret := range []string{"GITHUB-SECRET-FAKE", "BARK-PASSWORD-FAKE", "DEVICE-KEY-FAKE"} {
			if strings.Contains(s, secret) {
				t.Fatalf("formatting a Config or a Secret prints %s", secret)
			}
		}
	}
}

func TestConfigApproveOriginIsHTTPSAndMatchesRPID(t *testing.T) {
	for _, c := range []struct{ name, origin, rp string }{
		{"a plaintext approve origin", "http://approve.example.org", "approve.example.org"},
		{"the parent domain as RP ID", "https://approve.example.org", "example.org"},
		{"a sibling host as RP ID", "https://approve.example.org", "other.example.org"},
		{"an RP ID with a trailing dot and a port", "https://approve.example.org", "approve.example.org.:443"},
		{"an RP ID with a port", "https://approve.example.org", "approve.example.org:443"},
		{"a URL as RP ID", "https://approve.example.org", "https://approve.example.org/"},
		{"an upper-case RP ID", "https://approve.example.org", "Approve.Example.org"},
	} {
		_, err := LoadConfig(envOf(with("YH_APPROVE_ORIGIN", c.origin, "YH_WEBAUTHN_RP_ID", c.rp)))
		want := "YH_WEBAUTHN_RP_ID"
		if strings.HasPrefix(c.origin, "http:") {
			want = "YH_APPROVE_ORIGIN"
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err=%v, want %s named", c.name, err, want)
		}
	}
	for _, ok := range []struct{ origin, rp string }{
		{"https://approve.example.org", "approve.example.org"},
		{"https://approve.example.org:8443", "approve.example.org"},
		{"https://Approve.Example.org.", "approve.example.org"},
	} {
		if _, err := LoadConfig(envOf(with("YH_APPROVE_ORIGIN", ok.origin, "YH_WEBAUTHN_RP_ID", ok.rp))); err != nil {
			t.Fatalf("%+v: %v", ok, err)
		}
	}
}

func TestConfigRefusesSameHostOnDefaultHTTPPort(t *testing.T) {
	if _, err := LoadConfig(envOf(with("YH_PUBLIC_URL", "http://bus.example.org:80", "YH_APPROVE_ORIGIN", "https://bus.example.org", "YH_WEBAUTHN_RP_ID", "bus.example.org"))); err == nil {
		t.Fatal("http://bus.example.org:80 and https://bus.example.org are one host")
	}
}
