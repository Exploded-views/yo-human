package problem_test

import (
	"testing"

	"github.com/Exploded-views/yo-human/internal/problem"
)

func TestFlatLine(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"plain", "look now", "look now"},
		{"trims", "  x \t", "x"},
		{"zero-width space", "\u200b", ""},
		{"control", "\x01", ""},
		{"escape inside", "a\x1bb", "a b"},
		{"line separators", "a\u2028b\u2029c", "a b c"},
		{"bidi override", "a\u202eb", "a b"},
		{"newline", "a\nb", "a b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := problem.FlatLine(tc.in); got != tc.want {
				t.Errorf("FlatLine(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
