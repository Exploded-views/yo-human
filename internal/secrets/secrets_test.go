package secrets_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/problem"
	"github.com/Exploded-views/yo-human/internal/secrets"
)

// privateFile returns a path inside a fresh 0700 directory.
func privateFile(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "yo-human")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "secrets.yaml")
}

func mustSet(t *testing.T, path, name, value string) {
	t.Helper()
	if err := secrets.Set(path, name, []byte(value)); err != nil {
		t.Fatalf("Set %q: %v", name, err)
	}
}

func wantConfigProblem(t *testing.T, err error) *problem.Problem {
	t.Helper()
	p, ok := problem.As(err)
	if !ok {
		t.Fatalf("error = %v, want a *problem.Problem", err)
	}
	if p.Kind != problem.Config {
		t.Fatalf("Kind = %v, want Config", p.Kind)
	}
	return p
}

func TestSecretsPermissionsRefused(t *testing.T) {
	t.Run("file", func(t *testing.T) {
		path := privateFile(t)
		mustSet(t, path, "pd-critical", "ROUTING-FAKE-ONE")
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := secrets.Load(path)
		p := wantConfigProblem(t, err)
		if want := "chmod 600 " + path; !strings.Contains(p.Fix, want) {
			t.Errorf("Fix = %q, want it to contain %q", p.Fix, want)
		}
	})
	t.Run("directory", func(t *testing.T) {
		path := privateFile(t)
		mustSet(t, path, "pd-critical", "ROUTING-FAKE-ONE")
		dir := filepath.Dir(path)
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := secrets.Load(path)
		p := wantConfigProblem(t, err)
		if want := "chmod 700 " + dir; !strings.Contains(p.Fix, want) {
			t.Errorf("Fix = %q, want it to contain %q", p.Fix, want)
		}
	})
	t.Run("set refuses too", func(t *testing.T) {
		path := privateFile(t)
		mustSet(t, path, "a", "VALUE-FAKE-ONE")
		if err := os.Chmod(path, 0o666); err != nil {
			t.Fatal(err)
		}
		_ = wantConfigProblem(t, secrets.Set(path, "b", []byte("VALUE-FAKE-TWO")))
	})
}

func TestSecretsMissingFileIsEmpty(t *testing.T) {
	got, err := secrets.Load(privateFile(t))
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("Load of a missing file = %v, %v; want an empty non-nil map", got, err)
	}
	got, err = secrets.Load(filepath.Join(t.TempDir(), "nodir", "secrets.yaml"))
	if err != nil || len(got) != 0 {
		t.Fatalf("Load with a missing directory = %v, %v; want an empty map", got, err)
	}
}

func TestSecretsSetTrimsAndRefusesEmpty(t *testing.T) {
	path := privateFile(t)
	mustSet(t, path, "pd-critical", "ROUTING-FAKE-ONE \r\n\t\n")
	got, err := secrets.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got["pd-critical"] != "ROUTING-FAKE-ONE" {
		t.Errorf("value = %q, want trailing whitespace trimmed", got["pd-critical"])
	}
	for _, v := range []string{"", " \n\t"} {
		err := secrets.Set(path, "other", []byte(v))
		if p, ok := problem.As(err); !ok || p.Kind != problem.Usage {
			t.Errorf("Set(%q) = %v, want a Usage problem", v, err)
		}
	}
	if got, _ := secrets.Load(path); len(got) != 1 {
		t.Errorf("refused values must not be stored: %v", len(got))
	}
}

func TestSecretsSetRefusesBadNames(t *testing.T) {
	path := privateFile(t)
	long := strings.Repeat("a", 64)
	for _, name := range []string{"", "-lead", "Upper", "has space", "a/b", "dot.name", "a\nb", long} {
		err := secrets.Set(path, name, []byte("VALUE-FAKE-ONE"))
		if p, ok := problem.As(err); !ok || p.Kind != problem.Usage {
			t.Errorf("Set name %q = %v, want a Usage problem", name, err)
		}
	}
	for _, name := range []string{"a", "0", "pd-critical", strings.Repeat("g", 63)} {
		if err := secrets.Set(path, name, []byte("VALUE-FAKE-ONE")); err != nil {
			t.Errorf("Set name %q: %v", name, err)
		}
	}
}

func TestSecretsSetWritesMode0600AndCreatesDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "yo-human", "secrets.yaml")
	mustSet(t, path, "a", "VALUE-FAKE-ONE")
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v, %v; want mode 600", fi, err)
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil || di.Mode().Perm() != 0o700 {
		t.Fatalf("dir: %v, %v; want mode 700", di, err)
	}
	mustSet(t, path, "b", "VALUE-FAKE-TWO")
	mustSet(t, path, "a", "VALUE-FAKE-THREE")
	got, _ := secrets.Load(path)
	if len(got) != 2 || got["a"] != "VALUE-FAKE-THREE" || got["b"] != "VALUE-FAKE-TWO" {
		t.Errorf("secrets = %v, want b kept and a replaced", len(got))
	}
}

func TestSecretsMultilineValueRoundTrips(t *testing.T) {
	path := privateFile(t)
	mustSet(t, path, "cert", "LINE-FAKE-ONE\nLINE-FAKE-TWO: x # y\n")
	got, err := secrets.Load(path)
	if err != nil || got["cert"] != "LINE-FAKE-ONE\nLINE-FAKE-TWO: x # y" {
		t.Errorf("round trip = %q, %v", got["cert"], err)
	}
}

func TestSecretsListNeverReturnsValues(t *testing.T) {
	path := privateFile(t)
	mustSet(t, path, "zeta", "ZETA-FAKE-VALUE")
	mustSet(t, path, "alpha", "AL-FAKE")
	got, err := secrets.List(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []secrets.Entry{{Name: "alpha", Bytes: 7}, {Name: "zeta", Bytes: 15}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("List = %v, want %v (sorted by name)", got, want)
	}
	if s := fmt.Sprintf("%+v %#v", got, got); strings.Contains(s, "FAKE") {
		t.Errorf("List output holds a value: %s", s)
	}
}

func TestSecretsCorruptFileErrorHoldsNoValue(t *testing.T) {
	path := privateFile(t)
	body := "good: GOOD-FAKE-VALUE\nbad: [LEAK-FAKE-VALUE\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := secrets.Load(path)
	p := wantConfigProblem(t, err)
	for _, s := range []string{p.What, p.Valid, p.Fix, err.Error()} {
		if strings.Contains(s, "FAKE") {
			t.Errorf("error text holds file content: %q", s)
		}
	}
	if err := os.WriteFile(path, []byte("l: [a, b]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := secrets.Load(path); err == nil {
		t.Error("a list value must be refused")
	}
}

func TestSecretsValidName(t *testing.T) {
	for name, want := range map[string]bool{"a": true, "pd-critical": true, "TOKEN-FAKE-VALUE": false, "": false, "-a": false, "a.b": false, strings.Repeat("a", 64): false} {
		if got := secrets.ValidName(name); got != want {
			t.Errorf("ValidName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestSecretsSymlinkRefused(t *testing.T) {
	path := privateFile(t)
	target := filepath.Join(filepath.Dir(path), "real.yaml")
	if err := os.WriteFile(target, []byte("a: VALUE-FAKE-ONE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	_, err := secrets.Load(path)
	p := wantConfigProblem(t, err)
	if !strings.Contains(p.What, "symlink") || !strings.Contains(p.Fix, "rm "+problem.ShellQuote(path)) {
		t.Errorf("problem = %+v, want a symlink refusal with an rm fix", p)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(filepath.Dir(path), "missing"), path); err != nil {
		t.Fatal(err)
	}
	_ = wantConfigProblem(t, func() error { _, err := secrets.Load(path); return err }())
}

func TestSecretsNotARegularFileRefused(t *testing.T) {
	path := privateFile(t)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = wantConfigProblem(t, func() error { _, err := secrets.Load(path); return err }())
}

func TestSecretsOversizeRefused(t *testing.T) {
	path := privateFile(t)
	if err := os.WriteFile(path, []byte("a: "+strings.Repeat("a", 1<<20)), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = wantConfigProblem(t, func() error { _, err := secrets.Load(path); return err }())
}

// A hostile directory name must not leak into a fix as shell syntax or a forged line.
func TestSecretsFixQuotesHostilePaths(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "x;touch PWNED $(id) 'q'\nfix: forged\x1b[31m")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := secrets.Load(filepath.Join(dir, "secrets.yaml"))
	p := wantConfigProblem(t, err)
	if strings.ContainsAny(p.What+p.Valid+p.Fix, "\n\r\x1b") {
		t.Errorf("problem text holds a raw control character: %+v", p)
	}
	if want := "chmod 700 " + problem.ShellQuote(dir); !strings.Contains(p.Fix, want) {
		t.Errorf("Fix = %q, want it to contain %q", p.Fix, want)
	}
}

func TestSecretsValidNameRefusesLikelyTokens(t *testing.T) {
	for _, name := range []string{strings.Repeat("ab", 8), strings.Repeat("0f", 16), strings.Repeat("a1", 40)} {
		if secrets.ValidName(name) {
			t.Errorf("ValidName(%q) = true, want a hex string of 16+ characters refused as a likely token", name)
		}
		err := secrets.Set(privateFile(t), name, []byte("VALUE-FAKE-ONE"))
		p := wantUsage(t, err)
		if strings.Contains(p.What+p.Valid+p.Fix, name) {
			t.Errorf("Set error echoes the name: %+v", p)
		}
	}
	for _, name := range []string{strings.Repeat("ab", 7) + "a", "deadbeefcafe-backup", "abcdefabcdefabcdg"} {
		if !secrets.ValidName(name) {
			t.Errorf("ValidName(%q) = false, want true", name)
		}
	}
}

func wantUsage(t *testing.T, err error) *problem.Problem {
	t.Helper()
	p, ok := problem.As(err)
	if !ok || p.Kind != problem.Usage {
		t.Fatalf("error = %v, want a Usage problem", err)
	}
	return p
}

// A key that fails ValidName may be a pasted token; no function may hand its text back.
func TestSecretsInvalidStoredNamesAreNeverPrinted(t *testing.T) {
	path := privateFile(t)
	body := "good: GOOD-FAKE-VALUE\nTOKEN-FAKE-KEY: ONE-FAKE-VALUE\nbad key FAKE: TWO-FAKE-VALUE\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := secrets.Load(path)
	if err != nil || len(got) != 1 || got["good"] != "GOOD-FAKE-VALUE" {
		t.Fatalf("Load = %v, %v; want only the valid name", len(got), err)
	}
	list, err := secrets.List(path)
	if err != nil || len(list) != 3 {
		t.Fatalf("List = %v, %v; want 3 entries", list, err)
	}
	if s := fmt.Sprintf("%+v", list); strings.Contains(s, "FAKE-KEY") || strings.Contains(s, "bad key") || !strings.Contains(s, "invalid name") {
		t.Errorf("List shows an invalid name or hides the warning: %s", s)
	}
}

func TestSecretsSetKeepsEntriesItDoesNotUnderstand(t *testing.T) {
	path := privateFile(t)
	if err := os.WriteFile(path, []byte("good: GOOD-FAKE-VALUE\nTOKEN-FAKE-KEY: ONE-FAKE-VALUE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustSet(t, path, "other", "OTHER-FAKE-VALUE")
	again, _ := secrets.List(path)
	if len(again) != 3 {
		t.Errorf("Set must keep entries it does not understand, got %v", again)
	}
}
