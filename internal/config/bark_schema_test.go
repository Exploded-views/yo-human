package config

import (
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/registry"
)

// The bark settings schema must accept real instances: device_key holds a reference, which
// checkChannels swaps for a placeholder object before validating.
func TestBarkSchemaAcceptsSecretReferences(t *testing.T) {
	reg := channel.Registry{"bark": registry.New("test")["bark"]}
	ref := map[string]any{"secret": "k"}
	good := map[string]map[string]any{
		"default server": {"device_key": ref},
		"own server":     {"server": "https://bark.example", "device_key": ref},
	}
	for name, settings := range good {
		c := &Config{Channels: map[string]ChannelInstance{"phone": {Type: "bark", Settings: settings}}}
		if ps := checkChannels(c, reg); len(ps) != 0 {
			t.Errorf("%s: %v", name, ps)
		}
	}
	bad := map[string]map[string]any{
		"no key":      {"server": "https://bark.example"},
		"http server": {"server": "http://bark.example", "device_key": ref},
		"stray key":   {"device_key": ref, "x": 1},
		"literal":     {"device_key": "DEVICEKEY-FAKE-1"},
		"extra field": {"device_key": map[string]any{"secret": "k", "x": 1}},
	}
	for name, settings := range bad {
		c := &Config{Channels: map[string]ChannelInstance{"phone": {Type: "bark", Settings: settings}}}
		if ps := checkChannels(c, reg); len(ps) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
}
