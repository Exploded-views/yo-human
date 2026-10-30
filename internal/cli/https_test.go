package cli

import (
	"strings"
	"testing"
)

// config check refuses a Bark server or a PagerDuty events_url that is not plain https: the JSON
// schema stops http, and each type's own check stops what the schema's pattern lets through
// (credentials, a port with no host). The value is never echoed.
func TestConfigCheckRefusesChannelURLsThatAreNotHTTPS(t *testing.T) {
	cases := map[string]string{
		"bark http":         `  bell: { type: bark, server: "http://bark.example", device_key: { secret: bark-key } }`,
		"bark credentials":  `  bell: { type: bark, server: "https://u:p` + "@" + `bark.example", device_key: { secret: bark-key } }`, // split in the source so the public-repo scrub stays uniform
		"bark no host":      `  bell: { type: bark, server: "https://:443", device_key: { secret: bark-key } }`,
		"pagerduty http":    `  bell: { type: pagerduty, events_url: "http://events.example/v2", routing_key: { secret: pd-critical } }`,
		"pagerduty creds":   `  bell: { type: pagerduty, events_url: "https://u:p` + "@" + `events.example/v2", routing_key: { secret: pd-critical } }`, // split in the source so the public-repo scrub stays uniform
		"pagerduty no host": `  bell: { type: pagerduty, events_url: "https://:443/v2", routing_key: { secret: pd-critical } }`,
	}
	for name, line := range cases {
		s := newSandbox(t)
		write(t, s.userConfig(), strings.Replace(userConfigBody, "levels:\n", line+"\nlevels:\n", 1))
		code, out, errb := s.run("", "config", "check")
		if code != ExitConfig || out != "" || !strings.Contains(errb, "channels.bell") {
			t.Errorf("%s: code=%d out=%q err=%q; want exit 78 naming channels.bell", name, code, out, errb)
		}
		if strings.Contains(errb, "u:p") || strings.Contains(errb, "bark.example") || strings.Contains(errb, "events.example") {
			t.Errorf("%s: the value is echoed: %q", name, errb)
		}
	}
}
