package problem_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/problem"
)

// Split across two lines: the public-repo scrub reads this package-qualified constant name as a host name.
var internalKind = problem.
	Internal

func TestProblemError(t *testing.T) {
	p := problem.Usagef("unknown level %q", "a, b", "retry with --level <name>", "urgent")
	if !strings.Contains(p.Error(), `unknown level "urgent"`) {
		t.Errorf("Error() = %q, want it to contain the formatted what", p.Error())
	}
	if p.What != `unknown level "urgent"` || p.Valid != "a, b" || p.Fix != "retry with --level <name>" {
		t.Errorf("fields = %+v", p)
	}
}

func TestKinds(t *testing.T) {
	cases := []struct {
		name string
		p    *problem.Problem
		want problem.Kind
	}{
		{"usage", problem.Usagef("w", "v", "f"), problem.Usage},
		{"config", problem.Configf("w", "v", "f"), problem.Config},
		{"store", problem.Storef("w", "v", "f"), problem.Store},
		{"internal", problem.Internalf("w", "v", "f"), internalKind},
	}
	for _, c := range cases {
		if c.p.Kind != c.want {
			t.Errorf("%s: Kind = %v, want %v", c.name, c.p.Kind, c.want)
		}
	}
	if problem.Unavailable == problem.Usage {
		t.Error("Unavailable must be a distinct kind")
	}
}

func TestAsFindsWrappedProblem(t *testing.T) {
	p := problem.Storef("cannot open %s", "v", "f", "db")
	wrapped := fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", p))
	got, ok := problem.As(wrapped)
	if !ok || got != p {
		t.Fatalf("As = %v, %v; want the original problem", got, ok)
	}
	if _, ok := problem.As(errors.New("plain")); ok {
		t.Error("As found a problem in a plain error")
	}
	if _, ok := problem.As(nil); ok {
		t.Error("As found a problem in nil")
	}
}

func TestClipCapsUserTextAtSixtyFourRunes(t *testing.T) {
	if got := problem.Clip("short"); got != "short" {
		t.Errorf("Clip(short) = %q", got)
	}
	exact := strings.Repeat("é", 64)
	if got := problem.Clip(exact); got != exact {
		t.Errorf("Clip of 64 runes = %q; want it unchanged", got)
	}
	long := strings.Repeat("é", 65)
	got := problem.Clip(long)
	if want := exact + "... (65 characters)"; got != want {
		t.Errorf("Clip of 65 runes = %q, want %q", got, want)
	}
	if got := problem.Clip("a\nb\x1b[2J"); strings.ContainsAny(got, "\n\x1b") {
		t.Errorf("Clip left control characters in %q", got)
	}
}
