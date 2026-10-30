package problem_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/problem"
)

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"/a/b-c_d.yaml":   "/a/b-c_d.yaml",
		"":                "''",
		"a b":             "'a b'",
		"x;touch PWNED":   "'x;touch PWNED'",
		"$(id)":           "'$(id)'",
		"it's":            `'it'\''s'`,
		"a\nb":            `$'a\012b'`,
		"esc\x1b[31m":     `$'esc\033[31m'`,
		"q'\n":            `$'q\047\012'`,
		"back\\slash\x01": `$'back\134slash\001'`,
		"café x":          "'café x'",
		"-flag":           "'-flag'",
		"=x":              "'=x'",
		"a\xffb":          `$'a\377b'`,
		"\x9b":            `$'\233'`,
		"a'\xc3":          `$'a\047\303'`,
		"c1\u009b":        `$'c1\302\233'`,
		"x\\abc\x01":      `$'x\134abc\001'`,
		"\x01abcdef":      `$'\001abcdef'`,
		"rlo\u202e":       `$'rlo\342\200\256'`,
	}
	for in, want := range cases {
		got := problem.ShellQuote(in)
		if got != want {
			t.Errorf("ShellQuote(%q) = %q, want %q", in, got, want)
		}
		if strings.ContainsAny(got, "\n\r\x1b") {
			t.Errorf("ShellQuote(%q) = %q holds a raw control character", in, got)
		}
	}
}

var hostile = []string{
	"a';echo X;#\n",
	"a\xff';touch X;#\n",
	"\x9b';touch X;#",
	"=x'; touch X; #",
	"b\\';touch X;#\x01",
}

// shellOrSkip returns the path of a shell, or skips the test.
func shellOrSkip(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not installed", name)
	}
	return p
}

// Nothing a hostile name holds may run, in a shell with $'...' (bash) or without it (dash).
func TestShellQuoteExecutesNothing(t *testing.T) {
	for _, shell := range []string{"bash", "dash", "zsh", "ksh"} {
		sh := shellOrSkip(t, shell)
		for _, name := range hostile {
			dir := t.TempDir() // the shell runs here, so a stray file cannot land in the package
			marker := filepath.Join(dir, "PWNED")
			name = strings.ReplaceAll(name, "touch X", "touch "+marker)
			// the command's own status is irrelevant: a dash that reads a wrong literal may fail
			cmd := exec.Command(sh, "-c", "true "+problem.ShellQuote(name))
			cmd.Dir = dir
			_ = cmd.Run()
			if _, err := os.Stat(marker); err == nil {
				t.Errorf("%s executed part of %q (quoted %q)", shell, name, problem.ShellQuote(name))
			}
		}
	}
}

// In bash, zsh and ksh the quoted word names exactly the original bytes.
func TestShellQuoteRoundTrips(t *testing.T) {
	names := append([]string{"a b", "it's", "a\nb", "\x9b", "c1\u009b", "a\xffb\n", "=x", "-x", "x\\y", "caf\u00e9",
		"x\\abc\x01", "\x01abcdef", "\x1b9", "\n0a"}, hostile...)
	for _, shell := range []string{"bash", "zsh", "ksh"} {
		sh := shellOrSkip(t, shell)
		for _, name := range names {
			cmd := exec.Command(sh, "-c", "printf %s "+problem.ShellQuote(name))
			cmd.Dir = t.TempDir()
			out, err := cmd.Output()
			if err != nil || string(out) != name {
				t.Errorf("%s printed %q (%v) for %q, quoted %q", shell, out, err, name, problem.ShellQuote(name))
			}
		}
	}
}
