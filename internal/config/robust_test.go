package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/problem"
)

func userOnly(t *testing.T, cwd string) config.Paths {
	t.Helper()
	return config.Paths{Cwd: cwd, UserDir: abs(t, "testdata/user")}
}

func oneNotice(t *testing.T, notices []config.Notice, want string) string {
	t.Helper()
	if len(notices) != 1 || !strings.Contains(notices[0].Text, want) {
		t.Fatalf("notices = %+v, want one containing %q", notices, want)
	}
	if strings.ContainsAny(notices[0].Text, "\n\r\x1b") {
		t.Errorf("notice holds a raw control character: %q", notices[0].Text)
	}
	return notices[0].Text
}

func TestUnreadableProjectFileIsANoticeNotFatal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read a mode 000 file")
	}
	dir := canon(t, t.TempDir())
	f := write(t, dir, ".yo-human.yaml", "deadline: 9s\n")
	if err := os.Chmod(f, 0); err != nil {
		t.Fatal(err)
	}
	c, notices, err := config.Load(userOnly(t, dir), registry(), truster{})
	if err != nil {
		t.Fatalf("Load: %v, want the unreadable project file ignored", err)
	}
	oneNotice(t, notices, "cannot read the project config "+`"`+f+`"`)
	if c.Deadline != 3*time.Second {
		t.Errorf("Deadline = %v, want 3s from the user file", c.Deadline)
	}
}

func TestOversizeFiles(t *testing.T) {
	big := "# " + strings.Repeat("x", 1<<20) + "\n"
	dir := canon(t, t.TempDir())
	write(t, dir, ".yo-human.yaml", big)
	c, notices, err := config.Load(userOnly(t, dir), registry(), truster{})
	if err != nil || c == nil {
		t.Fatalf("Load: %v, want an oversize project file ignored", err)
	}
	oneNotice(t, notices, "larger than 1 MiB")
	p := config.Paths{Explicit: write(t, dir, "big.yaml", big), Cwd: filepath.Join(dir, "none")}
	_, _, err = config.Load(p, registry(), truster{})
	_ = wantProblem(t, err, problem.Config)
}

func TestTrustKeyIsTheSymlinkResolvedPath(t *testing.T) {
	root := canon(t, t.TempDir())
	real := write(t, root, "real/.yo-human.yaml", "deadline: 4s\n")
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(root, "real"), link); err != nil {
		t.Fatal(err)
	}
	c, notices, err := config.Load(userOnly(t, link), registry(), truster{real: sha(t, real)})
	if err != nil || len(notices) != 0 {
		t.Fatalf("Load: %v, notices %v; want the canonical path trusted through a symlinked cwd", err, notices)
	}
	if c.Deadline != 4*time.Second || c.Origin["deadline"] != real {
		t.Errorf("Deadline %v from %q, want 4s from %q", c.Deadline, c.Origin["deadline"], real)
	}
	// trusting only the symlinked spelling does not count
	_, notices, err = config.Load(userOnly(t, link), registry(), truster{filepath.Join(link, ".yo-human.yaml"): sha(t, real)})
	if err != nil {
		t.Fatal(err)
	}
	oneNotice(t, notices, "yo-human config trust")
}

func TestTrustKeyResolvesASymlinkedProjectFile(t *testing.T) {
	root := canon(t, t.TempDir())
	shared := write(t, root, "shared/team.yaml", "deadline: 6s\n")
	proj := filepath.Join(root, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, filepath.Join(proj, ".yo-human.yaml")); err != nil {
		t.Fatal(err)
	}
	c, notices, err := config.Load(userOnly(t, proj), registry(), truster{shared: sha(t, shared)})
	if err != nil || len(notices) != 0 || c.Deadline != 6*time.Second {
		t.Fatalf("Load: %v, notices %v; want the resolved file trusted", err, notices)
	}
}

// A hostile directory name must not become shell syntax or a forged line in a notice or a fix.
func TestHostilePathsAreQuoted(t *testing.T) {
	dir := filepath.Join(canon(t, t.TempDir()), "x;touch PWNED $(id) 'q'\nfix: forged\x1b[31m")
	f := write(t, dir, ".yo-human.yaml", "deadline: 9s\n")
	_, notices, err := config.Load(userOnly(t, dir), registry(), truster{})
	if err != nil {
		t.Fatal(err)
	}
	text := oneNotice(t, notices, "config trust "+problem.ShellQuote(f))
	if strings.Contains(text, "config trust "+f) {
		t.Errorf("notice %q holds the path unquoted", text)
	}
	bad := write(t, dir, "bad.yaml", "colour: red\n")
	_, _, err = config.Load(config.Paths{Explicit: bad, Cwd: filepath.Join(dir, "none"), UserDir: dir}, registry(), truster{})
	p := wantProblem(t, err, problem.Config)
	if strings.ContainsAny(p.What+p.Valid+p.Fix, "\n\r\x1b") {
		t.Errorf("problem text holds a raw control character: %+v", p)
	}
	if !strings.Contains(p.Fix, problem.ShellQuote(bad)) {
		t.Errorf("Fix = %q, want the quoted path", p.Fix)
	}
	_, _, err = config.Load(config.Paths{Explicit: filepath.Join(dir, "gone\n.yaml"), Cwd: dir}, registry(), truster{f: sha(t, f)})
	p = wantProblem(t, err, problem.Config)
	if strings.ContainsAny(p.What+p.Valid+p.Fix, "\n\r\x1b") {
		t.Errorf("missing-file problem holds a raw control character: %+v", p)
	}
}

func TestDurationsMustBePositive(t *testing.T) {
	for _, extra := range []string{"deadline: 0s\n", "deadline: -1s\n", "incident_ttl: 0s\n", "incident_ttl: -5m\n"} {
		_, _, err := loadExplicit(t, minimal+extra)
		p := wantProblem(t, err, problem.Config)
		if !strings.Contains(p.What, strings.SplitN(extra, ":", 2)[0]) {
			t.Errorf("%q: What = %q", extra, p.What)
		}
	}
	for _, every := range []string{"0s", "-1m"} {
		doc := strings.Replace(minimal, "use_when: testing", "use_when: t\n    retry: { every: "+every+" }", 1)
		_, _, err := loadExplicit(t, doc)
		if p := wantProblem(t, err, problem.Config); !strings.Contains(p.What, "retry.every") {
			t.Errorf("every %s: What = %q", every, p.What)
		}
	}
}

func TestNamesAreRestricted(t *testing.T) {
	for _, bad := range []string{"Bad_Name", "-lead", "has space", strings.Repeat("a", 64), "a.b"} {
		doc := strings.NewReplacer("n: {", `"`+bad+`": {`, "channel: n", `channel: "`+bad+`"`).Replace(minimal)
		if _, _, err := loadExplicit(t, doc); err == nil {
			t.Errorf("channel name %q accepted", bad)
		}
		doc = strings.Replace(minimal, "name: only", `name: "`+bad+`"`, 1)
		if _, _, err := loadExplicit(t, doc); err == nil {
			t.Errorf("level name %q accepted", bad)
		}
	}
}

func TestReferenceNamesAreValidated(t *testing.T) {
	c := valid()
	for _, name := range []string{"TOKEN-FAKE-VALUE", "has space", strings.Repeat("a", 64)} {
		c.Channels["phone"].Settings["token"] = map[string]any{"secret": name}
		_, err := c.Instances(map[string]string{name: "V"}, registry())
		p := wantProblem(t, err, problem.Config)
		if strings.Contains(p.What+p.Valid+p.Fix, name) || !strings.Contains(p.What, "must be a secret reference") ||
			!strings.Contains(p.Valid+p.Fix, "lower-case") {
			t.Errorf("problem for %q = %+v", name, p)
		}
		got := config.Check(c, registry())
		if len(got) != 1 || strings.Contains(got[0].What+got[0].Valid+got[0].Fix, name) {
			t.Errorf("Check for %q = %+v", name, got)
		}
	}
}

func TestLoadRunsCheck(t *testing.T) {
	bad := strings.Replace(minimal, "channel: n", "channel: ghost", 1)
	_, _, err := loadExplicit(t, bad)
	if p := wantProblem(t, err, problem.Config); !strings.Contains(p.What, `unknown channel "ghost"`) {
		t.Errorf("What = %q", p.What)
	}
	warn := strings.Replace(strings.Replace(minimal, "type: pager", "type: quiet", 1), "interruption: passive", "interruption: critical", 1)
	c, notices, err := loadExplicit(t, warn)
	if err != nil || c == nil {
		t.Fatalf("Load: %v, want a warning to load", err)
	}
	if len(notices) != 1 || !strings.Contains(notices[0].Text, "critical") {
		t.Errorf("notices = %+v, want the interruption warning", notices)
	}
}

func TestNoticesSurviveAnError(t *testing.T) {
	dir := canon(t, t.TempDir())
	write(t, dir, ".yo-human.yaml", "deadline: 9s\n")
	bad := write(t, dir, "bad.yaml", "colour: red\n")
	_, notices, err := config.Load(config.Paths{Explicit: bad, Cwd: dir}, registry(), truster{})
	if err == nil {
		t.Fatal("want an error")
	}
	oneNotice(t, notices, "yo-human config trust")
}

// A hex string this long is a pasted token, not a name; it must be refused and never echoed.
func TestReferenceThatLooksLikeAHexTokenIsRefused(t *testing.T) {
	c := valid()
	for _, name := range []string{strings.Repeat("ab", 16), strings.Repeat("0", 16)} {
		c.Channels["phone"].Settings["token"] = map[string]any{"secret": name}
		_, err := c.Instances(map[string]string{name: "V"}, registry())
		p := wantProblem(t, err, problem.Config)
		if strings.Contains(p.What+p.Valid+p.Fix, name) || !strings.Contains(p.What, "must be a secret reference") {
			t.Errorf("problem = %+v", p)
		}
	}
}
