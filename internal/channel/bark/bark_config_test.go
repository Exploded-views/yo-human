package bark

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
	if ty.Name != "bark" || ty.New == nil || len(ty.SettingsSchema) == 0 {
		t.Fatalf("type = %+v", ty)
	}
	if got := strings.Join(ty.SecretFields, ","); got != "device_key,username,password" {
		t.Errorf("SecretFields = %q", got)
	}
	checkCaps(t, ty.Capabilities)
	ch, err := ty.New(channel.Instance{Secrets: map[string]string{"device_key": fakeKey}})
	if err != nil || ch.Type() != "bark" || !reflect.DeepEqual(ch.Capabilities(), ty.Capabilities) {
		t.Errorf("channel = %v, %v", ch, err)
	}
}

func checkCaps(t *testing.T, c channel.Capabilities) {
	t.Helper()
	all := map[channel.Interruption]bool{channel.Passive: true, channel.Active: true, channel.TimeSensitive: true, channel.Critical: true}
	if !reflect.DeepEqual(c.Interruptions, all) {
		t.Errorf("Interruptions = %v", c.Interruptions)
	}
	flags := []bool{c.Subtitle, c.Link, c.ReachesHuman, !c.Markdown, !c.NativeDedup, !c.Resolve, !c.Schedule}
	if slices.Contains(flags, false) || c.MaxActions != 0 || c.MaxTitle != 200 || c.MaxBody != 4000 {
		t.Errorf("capabilities = %+v", c)
	}
}

func TestDefaultServer(t *testing.T) {
	got, err := parseServer(nil)
	if err != nil || got != "https://api.day.app" {
		t.Errorf("default = %q, %v", got, err)
	}
	if got, _ := parseServer("https://x.example/base/"); got != "https://x.example/base" {
		t.Errorf("trailing slash kept: %q", got)
	}
}

func TestNewValidatesSettings(t *testing.T) {
	ok := map[string]string{"device_key": fakeKey}
	cases := []struct {
		name     string
		settings map[string]any
		secrets  map[string]string
		field    string
	}{
		{"http server", map[string]any{"server": "http://x.example"}, ok, "server"},
		{"empty server", map[string]any{"server": ""}, ok, "server"},
		{"non-string server", map[string]any{"server": 5}, ok, "server"},
		{"no host", map[string]any{"server": "https://"}, ok, "server"},
		{"userinfo", map[string]any{"server": "https://u:p" + "@x.example"}, ok, "server"}, // split in the source so the public-repo scrub stays uniform
		{"query", map[string]any{"server": "https://x.example/?a=b"}, ok, "server"},
		{"empty query", map[string]any{"server": "https://x.example/base?"}, ok, "server"},
		{"fragment", map[string]any{"server": "https://x.example/#f"}, ok, "server"},
		{"no key", nil, map[string]string{}, "device_key"},
		{"key with space", nil, map[string]string{"device_key": fakeKey + " " + fakeKey}, "device_key"},
		{"key with newline", nil, map[string]string{"device_key": fakeKey + "\n"}, "device_key"},
		{"key with slash", nil, map[string]string{"device_key": fakeKey + "/" + fakeKey}, "device_key"},
	}
	for _, c := range cases {
		_, err := NewType("test").New(channel.Instance{Name: "n", Type: "bark", Settings: c.settings, Secrets: c.secrets})
		if err == nil || channel.KindOf(err) != channel.ConfigError {
			t.Errorf("%s: err=%v kind=%v", c.name, err, channel.KindOf(err))
			continue
		}
		if s := err.Error(); !strings.Contains(s, c.field) || strings.Contains(s, "FAKE") || strings.Contains(s, "u:p") || strings.Contains(s, "x.example") {
			t.Errorf("%s: error text %q", c.name, s)
		}
	}
	if _, err := NewType("test").New(channel.Instance{Settings: map[string]any{"server": "https://x.example/base/"}, Secrets: ok}); err != nil {
		t.Errorf("valid instance: %v", err)
	}
}

func TestValidateOptions(t *testing.T) {
	c, _ := newWithClient(channel.Instance{Secrets: map[string]string{"device_key": fakeKey}}, "1", nil)
	good := []map[string]any{nil, {}, {"sound": "minuet"}, {"call": true}, {"volume": 0}, {"volume": 10}, {"volume": 5.0}, {"volume": int64(5)}}
	for _, o := range good {
		if err := c.Validate(o); err != nil {
			t.Errorf("Validate(%v) = %v", o, err)
		}
	}
	bad := []map[string]any{{"volume": -1}, {"volume": 11}, {"volume": 5.5}, {"volume": "5"}, {"volume": true},
		{"call": "1"}, {"call": 1}, {"sound": 3}, {"sound": true}, {"tags": []any{"a"}}}
	for _, o := range bad {
		err := c.Validate(o)
		if err == nil || channel.KindOf(err) != channel.ConfigError {
			t.Errorf("Validate(%v) = %v", o, err)
		}
	}
	_, err := c.Send(context.Background(), msg(), channel.SendSpec{Interruption: channel.Active, Options: map[string]any{"volume": 99}})
	if err == nil || channel.KindOf(err) != channel.ConfigError {
		t.Errorf("Send with a bad option = %v", err)
	}
}

func TestResolveAndCancelUnsupported(t *testing.T) {
	c, _ := newWithClient(channel.Instance{Secrets: map[string]string{"device_key": fakeKey}}, "1", nil)
	if err := c.Resolve(context.Background(), "k", channel.Message{}); err == nil || channel.KindOf(err) != channel.PermanentError {
		t.Errorf("Resolve = %v", err)
	}
	if err := c.Cancel(context.Background(), channel.Receipt{}); err == nil || channel.KindOf(err) != channel.PermanentError {
		t.Errorf("Cancel = %v", err)
	}
}
