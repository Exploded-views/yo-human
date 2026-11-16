package app

import (
	"strings"
	"testing"
)

// TestConfigOriginAndLinkAllowListShapes (carried from Task 8's review): every YH_ORIGIN_ALLOW
// entry is an https origin exactly as a browser sends it, and every YH_LINK_HOSTS entry is a bare
// host. Any other entry is refused at start, and the error names the variable, never the value.
func TestConfigOriginAndLinkAllowListShapes(t *testing.T) {
	good := []struct{ name, value string }{
		{"YH_ORIGIN_ALLOW", "https://claude.ai"},
		{"YH_ORIGIN_ALLOW", "https://claude.ai,https://app.example.org:8443"},
		{"YH_LINK_HOSTS", "github.com"},
		{"YH_LINK_HOSTS", "github.com,docs.example.org"},
	}
	for _, c := range good {
		if _, err := LoadConfig(envOf(with(c.name, c.value))); err != nil {
			t.Errorf("%s=%q: %v, want it accepted", c.name, c.value, err)
		}
	}
	bad := []struct{ name, value string }{
		{"YH_ORIGIN_ALLOW", "https://claude.ai/"},
		{"YH_ORIGIN_ALLOW", "https://claude.ai/path"},
		{"YH_ORIGIN_ALLOW", "https://claude.ai?"},
		{"YH_ORIGIN_ALLOW", "https://claude.ai?q=1"},
		{"YH_ORIGIN_ALLOW", "https://claude.ai#frag"},
		{"YH_ORIGIN_ALLOW", "https://userinfo-MARK@claude.ai"},
		{"YH_ORIGIN_ALLOW", "https://claude.ai:443"},
		{"YH_ORIGIN_ALLOW", "https://claude.ai:"},
		{"YH_ORIGIN_ALLOW", "https://claude.ai:0443"},
		{"YH_ORIGIN_ALLOW", "https://claude.ai:99999"},
		{"YH_ORIGIN_ALLOW", "https://claude.ai:80a"},
		{"YH_ORIGIN_ALLOW", "http://claude.ai"},
		{"YH_ORIGIN_ALLOW", "claude.ai"},
		{"YH_ORIGIN_ALLOW", "https://Claude.ai"},
		{"YH_ORIGIN_ALLOW", "HTTPS://claude.ai"},
		{"YH_ORIGIN_ALLOW", "https://*.claude.ai"},
		{"YH_ORIGIN_ALLOW", "https://claude.ai,https://claude.com/"},
		{"YH_LINK_HOSTS", "https://github.com"},
		{"YH_LINK_HOSTS", "github.com:443"},
		{"YH_LINK_HOSTS", "github.com:8443"},
		{"YH_LINK_HOSTS", "*.github.com"},
		{"YH_LINK_HOSTS", "*"},
		{"YH_LINK_HOSTS", "github.com/"},
		{"YH_LINK_HOSTS", "github.com/path"},
		{"YH_LINK_HOSTS", "user-MARK@github.com"},
		{"YH_LINK_HOSTS", ".github.com"},
		{"YH_LINK_HOSTS", "github..com"},
		{"YH_LINK_HOSTS", "github.com,*.example.org"},
	}
	for _, c := range bad {
		_, err := LoadConfig(envOf(with(c.name, c.value)))
		if err == nil {
			t.Errorf("%s=%q accepted, want it refused", c.name, c.value)
			continue
		}
		if !strings.Contains(err.Error(), c.name) {
			t.Errorf("%s=%q: error %q does not name the variable", c.name, c.value, err)
		}
		if strings.Contains(err.Error(), "MARK") || strings.Contains(err.Error(), c.value) {
			t.Errorf("%s=%q: error %q prints the value", c.name, c.value, err)
		}
	}
}
