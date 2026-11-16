package app

import (
	"strings"
	"testing"
	"time"
)

// TestConfigToolDeadlineCeiling: the server's write timeout is 35 s and an answers wait is 25 s,
// so a tool deadline above 10 s would let the connection be cut under a call that may have
// committed.
func TestConfigToolDeadlineCeiling(t *testing.T) {
	if c := load(t, with("YH_TOOL_DEADLINE", "10s")); c.ToolDeadline != 10*time.Second {
		t.Errorf("ToolDeadline = %v, want 10s accepted", c.ToolDeadline)
	}
	for _, v := range []string{"10001ms", "11s", "35s", "1m", "0ms", "-1s", "soon"} {
		_, err := LoadConfig(envOf(with("YH_TOOL_DEADLINE", v)))
		if err == nil || !strings.Contains(err.Error(), "YH_TOOL_DEADLINE") || strings.Contains(err.Error(), v) {
			t.Errorf("YH_TOOL_DEADLINE=%q: err = %v, want it refused, naming the variable only", v, err)
		}
	}
}

// TestConfigLinkHostsAreDNSNames: an IP literal is not a link host, and names keep to the DNS
// lengths: 63 bytes a label and 253 in all.
func TestConfigLinkHostsAreDNSNames(t *testing.T) {
	label := strings.Repeat("a", 63)
	long := strings.Join([]string{label, label, label, strings.Repeat("a", 61)}, ".") // 253 bytes
	for _, v := range []string{"example.org", label + ".example.org", long} {
		if _, err := LoadConfig(envOf(with("YH_LINK_HOSTS", v))); err != nil {
			t.Errorf("YH_LINK_HOSTS=%.20q (%d bytes): %v, want it accepted", v, len(v), err)
		}
	}
	for _, v := range []string{"10.0.0.1", "127.0.0.1", "1.2.3.4", "999.999.999.999", "0x7f.1", "1234", strings.Repeat("a", 64) + ".example.org", long + "a", "[::1]", "::1"} {
		if _, err := LoadConfig(envOf(with("YH_LINK_HOSTS", v))); err == nil {
			t.Errorf("YH_LINK_HOSTS=%.20q accepted, want it refused", v)
		}
	}
	if _, err := LoadConfig(envOf(with("YH_ORIGIN_ALLOW", "https://10.0.0.1"))); err == nil {
		t.Error("an IP literal origin was accepted")
	}
}
