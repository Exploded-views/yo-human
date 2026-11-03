package problem

import (
	"strings"
	"unicode"
)

// OneLine keeps a value on its own line and free of control, format (BiDi overrides, joiners) and
// line-separator characters (a terminal would act on an escape sequence), so the error: / valid: /
// fix: shape cannot be forged. Each such character becomes a space.
func OneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, s)
}

// FlatLine makes s one line of plain text: OneLine, then trimmed. It is what a real page makes of
// an ask, so a check that asks "is it empty?" must use it.
func FlatLine(s string) string { return strings.TrimSpace(OneLine(s)) }
