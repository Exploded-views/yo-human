package config_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/problem"
)

const pagerSchema = `{"type":"object","properties":{
  "token":{"type":"object","required":["secret"],"properties":{"secret":{"type":"string"}},"additionalProperties":false},
  "topic":{"type":"string"}},"additionalProperties":false}`

func allInterruptions() map[channel.Interruption]bool {
	return map[channel.Interruption]bool{channel.Passive: true, channel.Active: true, channel.TimeSensitive: true, channel.Critical: true}
}

// registry has pager (everything, one secret field), quiet (passive and active only) and logonly
// (never reaches a human).
func registry() channel.Registry {
	return channel.Registry{
		"pager": {Name: "pager", SecretFields: []string{"token"}, SettingsSchema: []byte(pagerSchema),
			Capabilities: channel.Capabilities{ReachesHuman: true, Interruptions: allInterruptions()}},
		"quiet": {Name: "quiet",
			Capabilities: channel.Capabilities{ReachesHuman: true, Interruptions: map[channel.Interruption]bool{channel.Passive: true, channel.Active: true}}},
		"logonly": {Name: "logonly",
			Capabilities: channel.Capabilities{Interruptions: allInterruptions()}},
	}
}

// truster trusts exactly the (path, sha) pairs it was given.
type truster map[string]string

func (t truster) Trusted(path, sha string) (bool, error) { return t[path] == sha && sha != "", nil }

func sha(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func abs(t *testing.T, path string) string {
	t.Helper()
	a, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// write puts content in dir/name and returns the path.
func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const minimal = `version: 1
channels:
  n: { type: pager }
levels:
  - name: only
    use_when: testing
    send:
      - { channel: n, interruption: passive }
`

// loadExplicit loads one explicit file with nothing else on the search path.
func loadExplicit(t *testing.T, content string) (*config.Config, []config.Notice, error) {
	t.Helper()
	dir := t.TempDir()
	p := config.Paths{Explicit: write(t, dir, "c.yaml", content), Cwd: dir, UserDir: dir} // the user's own file, in their config directory
	return config.Load(p, registry(), truster{})
}

func mustLoadExplicit(t *testing.T, content string) *config.Config {
	t.Helper()
	c, _, err := loadExplicit(t, content)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return c
}

func wantProblem(t *testing.T, err error, kind problem.Kind) *problem.Problem {
	t.Helper()
	p, ok := problem.As(err)
	if !ok {
		t.Fatalf("error = %v, want a *problem.Problem", err)
	}
	if p.Kind != kind {
		t.Fatalf("Kind = %v, want %v (%s)", p.Kind, kind, p.What)
	}
	return p
}

// canon resolves symlinks, as the loader does for the project file.
func canon(t *testing.T, path string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(abs(t, path))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
