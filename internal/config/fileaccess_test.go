package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// OpenRegular opens a regular file once and refuses anything else, telling the refusal apart.
func TestOpenRegular(t *testing.T) {
	dir := t.TempDir()
	f, fi, err := config.OpenRegular(write(t, dir, "c.yaml", "version: 1\n"))
	if err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("OpenRegular of a regular file: %v", err)
	}
	_ = f.Close()
	if _, _, err := config.OpenRegular(dir); !config.IsNotRegular(err) {
		t.Errorf("OpenRegular of a directory: %v; want the not-regular refusal", err)
	}
	if _, _, err := config.OpenRegular(filepath.Join(dir, "gone")); err == nil || config.IsNotRegular(err) {
		t.Errorf("OpenRegular of a missing file: %v; want another error", err)
	}
}

// A relative HOME is no home: the project search then has no HOME stop.
func TestDefaultPathsIgnoreARelativeHome(t *testing.T) {
	t.Setenv("HOME", "relative")
	if got := config.DefaultPaths("/work").Home; got != "" {
		t.Errorf("Home = %q, want empty for a relative HOME", got)
	}
}

// With no user config directory (none configured, or none on disk), an explicit file is never
// the user's own: it needs trust.
func TestExplicitFileWithNoUserDirNeedsTrust(t *testing.T) {
	dir := canon(t, t.TempDir())
	path := write(t, dir, "c.yaml", minimal)
	for _, userDir := range []string{"", filepath.Join(dir, "missing")} {
		_, _, err := config.Load(config.Paths{Explicit: path, Cwd: dir, UserDir: userDir}, registry(), truster{})
		wantUntrustedExplicit(t, err, path, "user dir "+userDir)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

// CheckLayer is the schema check Load runs on every layer, for one file's bytes.
func TestCheckLayer(t *testing.T) {
	if err := config.CheckLayer("ok.yaml", []byte("version: 1\ndeadline: 2s\n")); err != nil {
		t.Errorf("a valid file: %v", err)
	}
	err := config.CheckLayer("bad.yaml", []byte("version: 1\ncolour: red\n"))
	if p, ok := problem.As(err); !ok || p.Kind != problem.Config || !strings.Contains(p.What, "breaks the config schema") {
		t.Errorf("a file with an unknown key: %v; want a Config problem", err)
	}
}

// A syntax error names the line the parser reports (yamlLine, compiled on first use) and quotes
// no file content.
func TestCheckLayerSyntaxErrorNamesTheLine(t *testing.T) {
	err := config.CheckLayer("bad.yaml", []byte("version: 1\ndeadline: [2s\n"))
	p, ok := problem.As(err)
	if !ok || p.Kind != problem.Config || !strings.Contains(p.What, "is not valid YAML or JSON at line ") {
		t.Fatalf("a syntax error: %v; want a Config problem that names the line", err)
	}
	if strings.Contains(p.What, "2s") {
		t.Errorf("the problem quotes the file: %q", p.What)
	}
}
