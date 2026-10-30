// Package problem is the one error type every layer returns for a failure the human or the agent
// can act on: what went wrong, what is valid, and how to fix it. It is a leaf and imports no other
// internal package.
package problem

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Kind says which exit code a Problem maps to.
type Kind int

const (
	Usage Kind = iota
	Config
	Store
	Internal
	Unavailable
	Queued // work the call needs is queued for another try; the result carries the next step
)

// Problem is the three-line error: What went wrong, Valid values, and the Fix.
type Problem struct {
	Kind             Kind
	What, Valid, Fix string
}

// Error is the "what" line; the CLI prints Valid and Fix on their own lines.
func (p *Problem) Error() string { return p.What }

func newProblem(k Kind, what, valid, fix string, args []any) *Problem {
	if len(args) > 0 {
		what = fmt.Sprintf(what, args...)
	}
	return &Problem{Kind: k, What: what, Valid: valid, Fix: fix}
}

// Usagef builds a Usage problem; what is a format string taking args.
func Usagef(what, valid, fix string, args ...any) *Problem {
	return newProblem(Usage, what, valid, fix, args)
}

// Configf builds a Config problem.
func Configf(what, valid, fix string, args ...any) *Problem {
	return newProblem(Config, what, valid, fix, args)
}

// Storef builds a Store problem.
func Storef(what, valid, fix string, args ...any) *Problem {
	return newProblem(Store, what, valid, fix, args)
}

// Internalf builds an Internal problem.
func Internalf(what, valid, fix string, args ...any) *Problem {
	return newProblem(Internal, what, valid, fix, args)
}

// As finds a *Problem anywhere in err's chain.
func As(err error) (*Problem, bool) {
	var p *Problem
	if errors.As(err, &p) {
		return p, true
	}
	return nil, false
}

// ShellQuote quotes s as one shell word on a single line, for a path inside a runnable fix.
//
// Plain text passes through. Text with shell syntax, or a leading "-" or "=", gets single quotes.
// Text that is not valid UTF-8, or holds a control character (C0, DEL or C1) or another
// non-printing character, becomes a $'...' word in which every such byte, every quote and every
// backslash is written as a three-digit octal escape (\NNN, which no shell reads greedily), walking the string byte by byte so invalid bytes survive intact.
//
// Contract: in bash, zsh and ksh the word names exactly the original bytes. A POSIX sh without
// $'...' (dash) reads it as a $ and a single-quoted literal, which may name a wrong path but can
// never run anything, because the word holds no quote, newline or other control byte inside it.
func ShellQuote(s string) string {
	if s != "" && s[0] != '-' && s[0] != '=' && strings.Trim(s, safeChars) == "" {
		return s
	}
	if utf8.ValidString(s) && strings.IndexFunc(s, unprintable) < 0 {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return ansiQuote(s)
}

// ansiQuote writes s as a $'...' word, escaping byte by byte.
func ansiQuote(s string) string {
	var b strings.Builder
	b.WriteString("$'")
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		escape := invalidByte(r, size) || r == '\'' || r == '\\' || unprintable(r)
		for _, c := range []byte(s[i : i+size]) {
			if escape {
				fmt.Fprintf(&b, `\%03o`, c)
			} else {
				b.WriteByte(c)
			}
		}
		i += size
	}
	b.WriteByte('\'')
	return b.String()
}

const safeChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-"

// unprintable covers C0, DEL, C1 and everything else unicode does not call printable.
func unprintable(r rune) bool { return r != ' ' && !unicode.IsPrint(r) }

func invalidByte(r rune, size int) bool { return r == utf8.RuneError && size == 1 }

// maxEcho is how much of a user's text an error message repeats.
const maxEcho = 64

// Clip shortens user text for an error message: control characters become spaces, and text longer
// than 64 characters is cut there and followed by its full length. A message never echoes more.
func Clip(s string) string {
	rs := []rune(s)
	cut := rs
	if len(rs) > maxEcho {
		cut = rs[:maxEcho]
	}
	out := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, string(cut))
	if len(rs) > maxEcho {
		out += fmt.Sprintf("... (%d characters)", len(rs))
	}
	return out
}
