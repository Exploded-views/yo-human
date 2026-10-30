package ntfy

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
	if ty.Name != "ntfy" || ty.New == nil || len(ty.SettingsSchema) == 0 {
		t.Fatalf("type = %+v", ty)
	}
	if got := strings.Join(ty.SecretFields, ","); got != "topic,token" {
		t.Errorf("SecretFields = %q", got)
	}
	checkCaps(t, ty.Capabilities)
	ch, err := newWithClient(channel.Instance{Settings: map[string]any{"server": "https://x.example"},
		Secrets: map[string]string{"topic": fakeTopic}}, "1", nil)
	if err != nil || ch.Type() != "ntfy" || ch.Capabilities().MaxBody != 4000 {
		t.Errorf("channel = %v, %v", ch, err)
	}
}

func checkCaps(t *testing.T, c channel.Capabilities) {
	t.Helper()
	want := map[channel.Interruption]bool{channel.Passive: true, channel.Active: true, channel.TimeSensitive: true}
	if !reflect.DeepEqual(c.Interruptions, want) {
		t.Errorf("Interruptions = %v", c.Interruptions)
	}
	flags := []bool{c.Markdown, c.Link, c.Schedule, c.ReachesHuman, !c.Resolve, !c.NativeDedup, !c.Subtitle}
	if slices.Contains(flags, false) || c.MaxActions != 3 || c.MaxTitle != 200 || c.MaxBody != 4000 {
		t.Errorf("capabilities = %+v", c)
	}
}

func TestNewValidatesSettings(t *testing.T) {
	ok := map[string]string{"topic": fakeTopic}
	cases := []struct {
		name     string
		settings map[string]any
		secrets  map[string]string
		field    string
	}{
		{"no server", map[string]any{}, ok, "server"},
		{"http server", map[string]any{"server": "http://x.example"}, ok, "server"},
		{"no host", map[string]any{"server": "https://"}, ok, "server"},
		{"userinfo", map[string]any{"server": "https://u:p" + "@x.example"}, ok, "server"}, // split in the source so the public-repo scrub stays uniform
		{"an empty query", map[string]any{"server": "https://x.example?"}, ok, "server"},
		{"a port and no host", map[string]any{"server": "https://:443"}, ok, "server"},
		{"no topic", map[string]any{"server": "https://x.example"}, map[string]string{}, "topic"},
		{"bad topic", map[string]any{"server": "https://x.example"}, map[string]string{"topic": fakeTopic + "/" + fakeTopic}, "topic"},
		{"bad token", map[string]any{"server": "https://x.example"}, map[string]string{"topic": fakeTopic, "token": fakeToken + "\n"}, "token"},
	}
	for _, c := range cases {
		_, err := NewType("test").New(channel.Instance{Name: "n", Type: "ntfy", Settings: c.settings, Secrets: c.secrets})
		if err == nil || channel.KindOf(err) != channel.ConfigError {
			t.Errorf("%s: err=%v kind=%v", c.name, err, channel.KindOf(err))
			continue
		}
		s := err.Error()
		if !strings.Contains(s, c.field) || strings.Contains(s, "FAKE") || strings.Contains(s, "u:p") {
			t.Errorf("%s: error text %q", c.name, s)
		}
	}
	if _, err := NewType("test").New(channel.Instance{Settings: map[string]any{"server": "https://x.example/base/"},
		Secrets: map[string]string{"topic": fakeTopic, "token": fakeToken}}); err != nil {
		t.Errorf("valid instance: %v", err)
	}
}

func TestValidateOptions(t *testing.T) {
	c, _ := newWithClient(channel.Instance{Settings: map[string]any{"server": "https://x.example"},
		Secrets: map[string]string{"topic": fakeTopic}}, "1", nil)
	good := []map[string]any{nil, {}, {"tags": []any{"a", "b"}}, {"tags": []string{"a"}}, {"markdown": true}}
	for _, o := range good {
		if err := c.Validate(o); err != nil {
			t.Errorf("Validate(%v) = %v", o, err)
		}
	}
	bad := []map[string]any{{"tags": "a"}, {"tags": []any{1}}, {"markdown": "yes"}, {"sound": "x"}}
	for _, o := range bad {
		if err := c.Validate(o); err == nil || channel.KindOf(err) != channel.ConfigError {
			t.Errorf("Validate(%v) = %v", o, err)
		}
	}
}

func TestResolveUnsupported(t *testing.T) {
	c, _ := newWithClient(channel.Instance{Settings: map[string]any{"server": "https://x.example"},
		Secrets: map[string]string{"topic": fakeTopic}}, "1", nil)
	if err := c.Resolve(context.Background(), "k", channel.Message{}); err == nil || channel.KindOf(err) != channel.PermanentError {
		t.Errorf("Resolve = %v", err)
	}
}
