package config

import (
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/registry"
)

// The ntfy settings schema must accept real instances: secret fields hold references, which
// checkChannels swaps for a placeholder object before validating.
func TestNtfySchemaAcceptsSecretReferences(t *testing.T) {
	reg := channel.Registry{"ntfy": registry.New("test")["ntfy"]}
	ref := func(n string) map[string]any { return map[string]any{"secret": n} }
	good := map[string]map[string]any{
		"topic only":      {"server": "https://ntfy.example", "topic": ref("t")},
		"topic and token": {"server": "https://ntfy.example", "topic": ref("t"), "token": ref("k")},
	}
	for name, settings := range good {
		c := &Config{Channels: map[string]ChannelInstance{"phone": {Type: "ntfy", Settings: settings}}}
		if ps := checkChannels(c, reg); len(ps) != 0 {
			t.Errorf("%s: %v", name, ps)
		}
	}
	bad := map[string]map[string]any{
		"no topic":    {"server": "https://ntfy.example"},
		"http server": {"server": "http://ntfy.example", "topic": ref("t")},
		"stray key":   {"server": "https://ntfy.example", "topic": ref("t"), "x": 1},
		"literal":     {"server": "https://ntfy.example", "topic": "TOPIC-FAKE-1"},
	}
	for name, settings := range bad {
		c := &Config{Channels: map[string]ChannelInstance{"phone": {Type: "ntfy", Settings: settings}}}
		if ps := checkChannels(c, reg); len(ps) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
}
