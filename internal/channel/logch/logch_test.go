package logch_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/logch"
)

func newLog(t *testing.T, path string) channel.Channel {
	t.Helper()
	ch, err := logch.Type.New(channel.Instance{Name: "local", Type: "log", Settings: map[string]any{"path": path}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ch
}

func readLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var lines []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("line %q is not JSON: %v", sc.Text(), err)
		}
		lines = append(lines, m)
	}
	return lines
}

func checkLines(t *testing.T, lines []map[string]any) {
	t.Helper()
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	for i, id := range []string{"01A", "01B"} {
		l := lines[i]
		if l["instance"] != "local" || l["interruption"] != "critical" {
			t.Errorf("line %d = %v", i, l)
		}
		if _, ok := l["time"].(string); !ok {
			t.Errorf("line %d has no time: %v", i, l)
		}
		msg, _ := l["message"].(map[string]any)
		if msg["id"] != id {
			t.Errorf("line %d message = %v, want id %s", i, l["message"], id)
		}
	}
}

func TestLogAppendsOneLinePerSend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yo.log")
	ch := newLog(t, path)
	for _, id := range []string{"01A", "01B"} {
		_, err := ch.Send(context.Background(), channel.Message{ID: id, Title: "t"},
			channel.SendSpec{Instance: "local", Interruption: channel.Critical})
		if err != nil {
			t.Fatalf("Send: %v", err)
		}
	}
	lines := readLines(t, path)
	checkLines(t, lines)
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestLogMissingPathIsConfigError(t *testing.T) {
	for name, settings := range map[string]map[string]any{
		"absent": {}, "nil": nil, "empty": {"path": ""}, "not a string": {"path": 7},
	} {
		_, err := logch.Type.New(channel.Instance{Name: "x", Type: "log", Settings: settings})
		if err == nil || channel.KindOf(err) != channel.ConfigError {
			t.Errorf("%s: err = %v (kind %v), want a ConfigError", name, err, channel.KindOf(err))
		}
	}
}

func TestLogUnwritablePathIsAnError(t *testing.T) {
	ch := newLog(t, filepath.Join(t.TempDir(), "no-such-dir", "yo.log"))
	if _, err := ch.Send(context.Background(), channel.Message{}, channel.SendSpec{}); err == nil {
		t.Error("send to an unwritable path succeeded")
	}
}

func TestLogCapabilities(t *testing.T) {
	c := newLog(t, "/nonexistent/FAKE.log").Capabilities()
	if c.ReachesHuman {
		t.Error("the log channel must never count as reaching a human")
	}
	for _, i := range []channel.Interruption{channel.Passive, channel.Active, channel.TimeSensitive, channel.Critical} {
		if !c.Interruptions[i] {
			t.Errorf("interruption %s not supported", i)
		}
	}
	if !c.Link || c.MaxActions != 3 || c.MaxTitle != 200 || c.MaxBody != 16384 {
		t.Errorf("capabilities = %+v", c)
	}
}

func TestLogTypeRegistration(t *testing.T) {
	if logch.Type.Name != "log" || newLog(t, "/nonexistent/FAKE.log").Type() != "log" {
		t.Error("type name must be log")
	}
	if logch.Type.Capabilities.ReachesHuman || !logch.Type.Capabilities.Link {
		t.Error("registry capabilities disagree with the channel's")
	}
	if !json.Valid(logch.Type.SettingsSchema) {
		t.Error("SettingsSchema is not valid JSON")
	}
}

func TestLogValidateAndUnsupported(t *testing.T) {
	ch := newLog(t, "/nonexistent/FAKE.log")
	if err := ch.Validate(nil); err != nil {
		t.Errorf("Validate(nil) = %v", err)
	}
	if err := ch.Validate(map[string]any{"bogus": 1}); channel.KindOf(err) != channel.ConfigError {
		t.Errorf("Validate(bogus) = %v, want a ConfigError", err)
	}
	if err := ch.Resolve(context.Background(), "k", channel.Message{}); channel.KindOf(err) != channel.PermanentError {
		t.Errorf("Resolve = %v, want permanent", err)
	}
	if err := ch.Cancel(context.Background(), channel.Receipt{}); channel.KindOf(err) != channel.PermanentError {
		t.Errorf("Cancel = %v, want permanent", err)
	}
}

func TestLogCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "yo.log")
	if _, err := newLog(t, path).Send(ctx, channel.Message{}, channel.SendSpec{}); err == nil {
		t.Error("send with a canceled context succeeded")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a canceled send wrote the file")
	}
}

// A log path must be absolute: a relative one would land wherever the caller's working directory
// happens to be.
func TestLogRelativePathIsConfigError(t *testing.T) {
	for _, path := range []string{"y.log", "./y.log", "../y.log", "logs/y.log", "~/.zshrc"} {
		_, err := logch.Type.New(channel.Instance{Name: "x", Type: "log", Settings: map[string]any{"path": path}})
		if err == nil || channel.KindOf(err) != channel.ConfigError || !strings.Contains(err.Error(), "absolute") {
			t.Errorf("%s: err = %v (kind %v), want a ConfigError asking for an absolute path", path, err, channel.KindOf(err))
		}
	}
}
