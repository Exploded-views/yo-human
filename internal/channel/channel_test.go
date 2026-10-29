package channel_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
)

func TestKindOf(t *testing.T) {
	base := errors.New("x")
	cases := []struct {
		name string
		err  error
		want channel.ErrorKind
	}{
		{"permanent", channel.Permanent(base), channel.PermanentError},
		{"transient", channel.Transient(base), channel.TransientError},
		{"config", channel.Misconfigured(base), channel.ConfigError},
		{"plain counts as transient", base, channel.TransientError},
		{"permanent through %w", fmt.Errorf("send: %w", channel.Permanent(base)), channel.PermanentError},
		{"config through two %w", fmt.Errorf("a: %w", fmt.Errorf("b: %w", channel.Misconfigured(base))), channel.ConfigError},
	}
	for _, c := range cases {
		if got := channel.KindOf(c.err); got != c.want {
			t.Errorf("%s: KindOf = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestKindedErrorKeepsCause(t *testing.T) {
	base := errors.New("boom")
	err := channel.Permanent(base)
	if !errors.Is(err, base) {
		t.Error("the marked error must unwrap to its cause")
	}
	if err.Error() != "boom" {
		t.Errorf("Error() = %q, want the cause's text", err.Error())
	}
	for _, mark := range []func(error) error{channel.Transient, channel.Permanent, channel.Misconfigured} {
		if mark(nil) != nil {
			t.Error("marking nil must stay nil")
		}
	}
}

func TestParseInterruption(t *testing.T) {
	for _, s := range []string{"passive", "active", "time-sensitive", "critical"} {
		got, err := channel.ParseInterruption(s)
		if err != nil || string(got) != s {
			t.Errorf("ParseInterruption(%q) = %q, %v", s, got, err)
		}
	}
	_, err := channel.ParseInterruption("loud")
	if err == nil {
		t.Fatal("ParseInterruption(loud) did not fail")
	}
	for _, want := range []string{`"loud"`, "passive", "active", "time-sensitive", "critical"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestInterruptionConstants(t *testing.T) {
	want := map[channel.Interruption]string{
		channel.Passive: "passive", channel.Active: "active",
		channel.TimeSensitive: "time-sensitive", channel.Critical: "critical",
	}
	for got, s := range want {
		if string(got) != s {
			t.Errorf("constant %q, want %q", got, s)
		}
	}
}
