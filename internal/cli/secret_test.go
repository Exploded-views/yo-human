package cli

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestSecretSetReadsStdinOnly(t *testing.T) {
	s := newSandbox(t)
	// A value in the arguments is refused, and is not echoed or stored.
	r := s.do("FAKE-VALUE\n", "secret", "set", "pd-critical", "FAKE-ARG-VALUE")
	r.fails(t, "extra argument", ExitUsage, "error: ")
	mustNotContain(t, "stderr", r.err, "FAKE-ARG-VALUE", "FAKE-VALUE")
	_, err := os.Stat(s.secretsFile())
	expect(t, err != nil, "the secrets file exists after a refused set")

	r = s.do("FAKE-VALUE\n", "secret", "set", "pd-critical")
	r.succeeds(t, "set")
	expect(t, r.err == "", "set stderr = %q", r.err)
	mustNotContain(t, "set output", r.out+r.err, "FAKE-VALUE")
	fi, err := os.Stat(s.secretsFile())
	expect(t, err == nil && fi.Mode().Perm() == 0o600, "secrets file: %v, %v; want mode 0600", fi, err)
	body, _ := os.ReadFile(s.secretsFile())
	mustContain(t, "stored file", string(body), "pd-critical: FAKE-VALUE\n")
}

func TestSecretListShowsNamesAndSizes(t *testing.T) {
	s := newSandbox(t)
	s.do("FAKE-VALUE\n", "secret", "set", "pd-critical").succeeds(t, "set")
	r := s.do("", "secret", "list")
	expect(t, r.code == ExitOK && r.out == "RESULT ok\npd-critical 10 bytes\n" && r.err == "", "list: %+v", r)
	r = s.do("", "secret", "list", "--json")
	mustContain(t, "list json", r.out, `"secrets":[{"bytes":10,"name":"pd-critical"}]`)
	mustNotContain(t, "list json", r.out, "FAKE-VALUE")
	r = s.do("FAKE-VALUE\n", "secret", "set", "second", "--json")
	mustContain(t, "set json", r.out, `"name":"second"`)
}

func TestSecretSetRefusesBadNamesWithoutEchoingThem(t *testing.T) {
	s := newSandbox(t)
	for _, name := range []string{"Bad_Name", "FAKE-PASTED-VALUE", strings.Repeat("0123456789abcdef", 2), "-lead", strings.Repeat("a-", 40)} {
		r := s.do("FAKE-VALUE\n", "secret", "set", name)
		r.fails(t, "name "+name, ExitUsage, "error: that is not a valid secret name")
		mustNotContain(t, "stderr for "+name, r.err, name)
	}
	var out, errb strings.Builder // an invalid name is refused before stdin is read
	code := run([]string{"secret", "set", "Bad_Name"}, brokenReader{}, &out, &errb)
	expect(t, code == ExitUsage && strings.Contains(errb.String(), "not a valid secret name"), "invalid name with an unreadable stdin: code=%d err=%q", code, errb.String())
	s.do("FAKE-VALUE\n", "secret", "set", "").fails(t, "empty name", ExitUsage, "not a valid secret name")
	s.do("FAKE-VALUE\n", "secret", "set").fails(t, "no name", ExitUsage, "secret set takes one name")
	_, err := os.Stat(s.secretsFile())
	expect(t, err != nil, "the secrets file exists after only refused names")
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errors.New("pipe closed") }

func TestSecretSetStdinProblems(t *testing.T) {
	s := newSandbox(t)
	s.do("  \n", "secret", "set", "empty-one").fails(t, "empty value", ExitUsage, "is empty")
	s.do(strings.Repeat("a", maxSecretBytes+1), "secret", "set", "huge").fails(t, "huge value", ExitUsage, "longer than")
	var out, errb strings.Builder
	code := run([]string{"secret", "set", "broken"}, brokenReader{}, &out, &errb)
	expect(t, code == ExitUsage && strings.Contains(errb.String(), "cannot read the secret from stdin"), "unreadable stdin: code=%d err=%q; want exit 64", code, errb.String())
}

// A loose secrets file stops a write (exit 78) and a list, and never prints a value.
func TestSecretCommandsRefuseALooseSecretsFile(t *testing.T) {
	s := newSandbox(t)
	write(t, s.secretsFile(), "other: FAKE-OTHER\n")
	expect(t, os.Chmod(s.secretsFile(), 0o644) == nil, "chmod")
	for _, args := range [][]string{{"secret", "set", "fine-name"}, {"secret", "list"}} {
		r := s.do("FAKE-VALUE\n", args...)
		r.fails(t, strings.Join(args, " "), ExitConfig, "chmod 600")
		mustNotContain(t, "stderr", r.err, "FAKE-OTHER", "FAKE-VALUE")
	}
	s.do("", "secret", "list", "extra").fails(t, "list extra", ExitUsage, "secret list takes no arguments")
}

func TestSecretListShowsInvalidStoredNamesAsPlaceholders(t *testing.T) {
	s := newSandbox(t)
	write(t, s.secretsFile(), "pd-critical: FAKE-VALUE\nBad_Pasted_Name: FAKE-OTHER\n")
	r := s.do("", "secret", "list")
	r.succeeds(t, "list")
	mustContain(t, "list", r.out, "pd-critical 10 bytes", "<invalid name 1> 10 bytes")
	mustNotContain(t, "list", r.out+r.err, "Bad_Pasted_Name", "FAKE-VALUE", "FAKE-OTHER")
}

func TestSecretListWithoutAFileIsEmpty(t *testing.T) {
	r := newSandbox(t).do("", "secret", "list")
	expect(t, r.code == ExitOK && r.out == "RESULT ok\n", "list = %+v, want just RESULT ok", r)
}

// A terminal (or any character device) is refused: a typed or pasted value would sit in scrollback.
func TestSecretSetRefusesATerminal(t *testing.T) {
	s := newSandbox(t)
	tty, err := os.Open(os.DevNull)
	expect(t, err == nil, "open %s: %v", os.DevNull, err)
	defer func() { _ = tty.Close() }()
	var out, errb strings.Builder
	code := run([]string{"secret", "set", "pd-critical"}, tty, &out, &errb)
	expect(t, code == ExitUsage && out.Len() == 0, "code=%d out=%q err=%q; want exit 64", code, out.String(), errb.String())
	mustContain(t, "stderr", errb.String(), "stdin is a terminal or device, not a pipe", `op read "op://<vault>/<item>/<field>" | yo-human secret set pd-critical`)
	_, statErr := os.Stat(s.secretsFile())
	expect(t, statErr != nil, "the secrets file exists after a refused set")
}
