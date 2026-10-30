package pagerduty

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
)

func TestTypeDeclaration(t *testing.T) {
	ty := NewType("test")
	if ty.Name != "pagerduty" || ty.New == nil || len(ty.SettingsSchema) == 0 {
		t.Fatalf("type = %+v", ty)
	}
	if got := strings.Join(ty.SecretFields, ","); got != "routing_key" {
		t.Errorf("SecretFields = %q", got)
	}
	checkCaps(t, ty.Capabilities)
	ch, err := ty.New(channel.Instance{Secrets: map[string]string{"routing_key": fakeKey}})
	if err != nil || ch.Type() != "pagerduty" || !reflect.DeepEqual(ch.Capabilities(), ty.Capabilities) {
		t.Errorf("channel = %v, %v", ch, err)
	}
}

func checkCaps(t *testing.T, c channel.Capabilities) {
	t.Helper()
	all := map[channel.Interruption]bool{channel.Passive: true, channel.Active: true, channel.TimeSensitive: true, channel.Critical: true}
	if !reflect.DeepEqual(c.Interruptions, all) {
		t.Errorf("Interruptions = %v", c.Interruptions)
	}
	flags := []bool{c.Link, c.NativeDedup, c.Resolve, c.ReachesHuman, !c.Markdown, !c.Subtitle, !c.Schedule}
	if slices.Contains(flags, false) || c.MaxActions != 0 || c.MaxTitle != 1024 {
		t.Errorf("capabilities = %+v", c)
	}
}

func TestDefaultEventsURL(t *testing.T) {
	got, err := parseEventsURL(nil)
	if err != nil || got != "https://events.pagerduty.com/v2/enqueue" {
		t.Errorf("default = %q, %v", got, err)
	}
}

func TestNewValidatesSettings(t *testing.T) {
	ok := map[string]string{"routing_key": fakeKey}
	cases := []struct {
		name     string
		settings map[string]any
		secrets  map[string]string
		field    string
	}{
		{"http url", map[string]any{"events_url": "http://x.example/v2"}, ok, "events_url"},
		{"empty url", map[string]any{"events_url": ""}, ok, "events_url"},
		{"non-string url", map[string]any{"events_url": 5}, ok, "events_url"},
		{"no host", map[string]any{"events_url": "https://"}, ok, "events_url"},
		{"empty hostname", map[string]any{"events_url": "https://:8080/v2"}, ok, "events_url"},
		{"userinfo", map[string]any{"events_url": "https://u:p" + "@x.example/v2"}, ok, "events_url"}, // split in the source so the public-repo scrub stays uniform
		{"query", map[string]any{"events_url": "https://x.example/v2?a=b"}, ok, "events_url"},
		{"empty query", map[string]any{"events_url": "https://x.example/v2?"}, ok, "events_url"},
		{"fragment", map[string]any{"events_url": "https://x.example/v2#f"}, ok, "events_url"},
		{"no key", nil, map[string]string{}, "routing_key"},
		{"empty key", nil, map[string]string{"routing_key": ""}, "routing_key"},
		{"key with space", nil, map[string]string{"routing_key": fakeKey + " " + fakeKey}, "routing_key"},
		{"key with newline", nil, map[string]string{"routing_key": fakeKey + "\n"}, "routing_key"},
	}
	for _, c := range cases {
		_, err := NewType("test").New(channel.Instance{Name: "n", Type: "pagerduty", Settings: c.settings, Secrets: c.secrets})
		if err == nil || channel.KindOf(err) != channel.ConfigError {
			t.Errorf("%s: err=%v kind=%v", c.name, err, channel.KindOf(err))
			continue
		}
		if s := err.Error(); !strings.Contains(s, c.field) || strings.Contains(s, "FAKE") || strings.Contains(s, "u:p") || strings.Contains(s, "x.example") {
			t.Errorf("%s: error text %q", c.name, s)
		}
	}
	if _, err := NewType("test").New(channel.Instance{Settings: map[string]any{"events_url": "https://x.example/v2/enqueue"}, Secrets: ok}); err != nil {
		t.Errorf("valid instance: %v", err)
	}
}

func TestValidateOptions(t *testing.T) {
	c, _ := newWithClient(channel.Instance{Secrets: map[string]string{"routing_key": fakeKey}}, "1", nil)
	for _, o := range []map[string]any{nil, {}} {
		if err := c.Validate(o); err != nil {
			t.Errorf("Validate(%v) = %v", o, err)
		}
	}
	err := c.Validate(map[string]any{"sound": "x"})
	if err == nil || channel.KindOf(err) != channel.ConfigError {
		t.Errorf("Validate with an option = %v", err)
	}
	_, err = c.Send(context.Background(), msg(), channel.SendSpec{Interruption: channel.Active, Options: map[string]any{"x": 1}})
	if err == nil || channel.KindOf(err) != channel.ConfigError {
		t.Errorf("Send with an option = %v", err)
	}
}

func TestCancelUnsupported(t *testing.T) {
	c, _ := newWithClient(channel.Instance{Secrets: map[string]string{"routing_key": fakeKey}}, "1", nil)
	if err := c.Cancel(context.Background(), channel.Receipt{}); err == nil || channel.KindOf(err) != channel.PermanentError {
		t.Errorf("Cancel = %v", err)
	}
}
