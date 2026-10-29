package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/problem"
)

func logRegistry() channel.Registry {
	reg := registry()
	reg["log"] = channel.Type{Name: "log", Capabilities: channel.Capabilities{Interruptions: allInterruptions()}}
	return reg
}

const withLog = `version: 1
channels:
  n: { type: pager }
  journal: { type: log, path: "%s" }
levels:
  - name: only
    use_when: testing
    send:
      - { channel: n, interruption: passive }
      - { channel: journal, interruption: passive }
`

// loadProjectAt loads the trusted project file at proj (found from root).
func loadProjectAt(t *testing.T, root, proj string) (*config.Config, error) {
	t.Helper()
	p := config.Paths{Cwd: root, UserDir: filepath.Join(root, "none")}
	real, err := filepath.EvalSymlinks(proj)
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := config.Load(p, logRegistry(), truster{real: sha(t, real)})
	return c, err
}

func wantProjectLogRefused(t *testing.T, err error, label string) {
	t.Helper()
	p := wantProblem(t, err, problem.Config)
	if !strings.Contains(p.What, "channels.journal") || !strings.Contains(p.Fix, "define log channels in your user config") || !strings.Contains(p.Fix, "config.yaml") {
		t.Errorf("%s: problem %+v; want one naming channels.journal with the user-config fix", label, p)
	}
}

// A project layer may not set a log channel's path at all, whatever the path says.
func TestProjectLayerMayNotSetALogPath(t *testing.T) {
	root := canon(t, t.TempDir())
	for _, path := range []string{"/etc/yo-human.log", "../out.log", "~/.zshrc", "logs/yo.log", "./y.log", filepath.Join(root, "abs.log")} {
		proj := write(t, root, ".yo-human.yaml", strings.Replace(withLog, "%s", path, 1))
		_, err := loadProjectAt(t, root, proj)
		wantProjectLogRefused(t, err, path)
	}
}

// The layer is known from how the file was found, not from its name.
func TestProjectLayerIsRecognisedWhateverTheFileIsCalled(t *testing.T) {
	root := canon(t, t.TempDir())
	body := strings.Replace(withLog, "%s", "logs/yo.log", 1)

	real := write(t, canon(t, t.TempDir()), "weird-name.cfg", body)
	if err := os.Symlink(real, filepath.Join(root, ".yo-human.yaml")); err != nil {
		t.Fatal(err)
	}
	_, err := loadProjectAt(t, root, filepath.Join(root, ".yo-human.yaml"))
	wantProjectLogRefused(t, err, "symlinked project file")

	jsonRoot := canon(t, t.TempDir())
	proj := write(t, jsonRoot, ".yo-human.json", `{"version":1,"channels":{"n":{"type":"pager"},"journal":{"type":"log","path":"y.log"}},`+
		`"levels":[{"name":"only","use_when":"t","send":[{"channel":"n","interruption":"passive"}]}]}`)
	_, err = loadProjectAt(t, jsonRoot, proj)
	wantProjectLogRefused(t, err, ".json project file")
}

func TestNonProjectLayersMayDefineLogChannels(t *testing.T) {
	dir := canon(t, t.TempDir())
	body := strings.Replace(withLog, "%s", "/var/log/yo-human.log", 1)
	// An explicit file, and a user file, even when named like a project file.
	for name, p := range map[string]config.Paths{
		"explicit": {Explicit: write(t, dir, "c.yaml", body), Cwd: dir, UserDir: dir},
		"user":     {UserDir: filepath.Join(dir, "user"), Cwd: dir},
	} {
		if name == "user" {
			write(t, p.UserDir, "config.yaml", body)
		}
		c, _, err := config.Load(p, logRegistry(), truster{})
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if c.Channels["journal"].Settings["path"] != "/var/log/yo-human.log" {
			t.Errorf("%s: settings = %v", name, c.Channels["journal"].Settings)
		}
	}
	named := write(t, dir, ".yo-human.yaml", body)
	c, _, err := config.Load(config.Paths{Explicit: named, Cwd: filepath.Join(dir, "elsewhere"), UserDir: dir}, logRegistry(), truster{})
	if err != nil {
		t.Fatalf("explicit file named like a project file: %v", err)
	}
	if c.OriginLayer["channels.journal"] != config.LayerExplicit {
		t.Errorf("layer = %v, want LayerExplicit", c.OriginLayer["channels.journal"])
	}
}

func TestOriginLayerRecordsTheKindOfEachLayer(t *testing.T) {
	c, _, err := config.Load(fixturePaths(t), registry(), trustedProject(t))
	if err != nil {
		t.Fatal(err)
	}
	if c.OriginLayer["channels.repo"] != config.LayerProject || c.OriginLayer["incident_ttl"] != config.LayerExplicitOutside {
		t.Errorf("OriginLayer = %v, want channels.repo from the project and incident_ttl from the explicit file outside the user directory", c.OriginLayer)
	}
}
func TestLoadReturnsEveryFindingJoined(t *testing.T) {
	_, _, err := loadExplicit(t, strings.Replace(minimal, "n: { type: pager }", "n: { type: smoke }\n  m: { type: signal }", 1))
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) || len(joined.Unwrap()) < 2 {
		t.Fatalf("err = %v; want a join of every finding", err)
	}
	for _, e := range joined.Unwrap() {
		_ = wantProblem(t, e, problem.Config)
	}
}

func TestWarningNoticesCarryTheirProblem(t *testing.T) {
	body := strings.Replace(minimal, "type: pager", "type: quiet", 1)
	body = strings.Replace(body, "interruption: passive", "interruption: critical", 1)
	_, notices, err := loadExplicit(t, body)
	if err != nil || len(notices) != 1 || notices[0].Problem == nil || !config.IsWarning(notices[0].Problem) || notices[0].Problem.Fix == "" {
		t.Errorf("notices = %+v, err %v; want one warning notice carrying its problem", notices, err)
	}
}

func TestProjectFileFindsTheNearestAncestorResolved(t *testing.T) {
	root := canon(t, t.TempDir())
	near := write(t, filepath.Join(root, "a"), ".yo-human.json", `{}`)
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(deep, link); err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []string{deep, link} {
		if got := config.ProjectFile(cwd); got != near {
			t.Errorf("ProjectFile(%s) = %q, want %q", cwd, got, near)
		}
	}
	if got := config.ProjectFile(canon(t, t.TempDir())); got != "" {
		t.Errorf("ProjectFile with none = %q, want empty", got)
	}
}
