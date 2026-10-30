package config

import (
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/registry"
)

// The pagerduty settings schema must accept real instances: routing_key holds a reference, which
// checkChannels swaps for a placeholder object before validating.
func TestPagerDutySchemaAcceptsSecretReferences(t *testing.T) {
	reg := channel.Registry{"pagerduty": registry.New("test")["pagerduty"]}
	ref := map[string]any{"secret": "k"}
	good := map[string]map[string]any{
		"default url": {"routing_key": ref},
		"own url":     {"events_url": "https://events.example/v2/enqueue", "routing_key": ref},
	}
	for name, settings := range good {
		c := &Config{Channels: map[string]ChannelInstance{"pager": {Type: "pagerduty", Settings: settings}}}
		if ps := checkChannels(c, reg); len(ps) != 0 {
			t.Errorf("%s: %v", name, ps)
		}
	}
	bad := map[string]map[string]any{
		"no key":      {"events_url": "https://events.example/v2/enqueue"},
		"http url":    {"events_url": "http://events.example", "routing_key": ref},
		"stray key":   {"routing_key": ref, "x": 1},
		"literal":     {"routing_key": "RK-FAKE-1"},
		"extra field": {"routing_key": map[string]any{"secret": "k", "x": 1}},
	}
	for name, settings := range bad {
		c := &Config{Channels: map[string]ChannelInstance{"pager": {Type: "pagerduty", Settings: settings}}}
		if ps := checkChannels(c, reg); len(ps) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
}
