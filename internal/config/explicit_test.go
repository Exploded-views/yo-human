package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// explicitWorld is a user config directory and, beside it, a directory a caller controls.
type explicitWorld struct{ userDir, outside, cwd string }

func newExplicitWorld(t *testing.T) explicitWorld {
	t.Helper()
	root := canon(t, t.TempDir())
	w := explicitWorld{userDir: filepath.Join(root, "config", "yo-human"), outside: filepath.Join(root, "repo"), cwd: filepath.Join(root, "cwd")}
	for _, d := range []string{w.userDir, w.outside, w.cwd} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

func (w explicitWorld) load(explicit string, trust truster) (*config.Config, []config.Notice, error) {
	return config.Load(config.Paths{Explicit: explicit, Cwd: w.cwd, UserDir: w.userDir}, logRegistry(), trust)
}

// wantUntrustedExplicit expects the refusal of an explicit file that is neither the user's own nor
// trusted: a config problem that names the file and tells the agent to hand the trust to the human.
func wantUntrustedExplicit(t *testing.T, err error, path, label string) {
	t.Helper()
	p := wantProblem(t, err, problem.Config)
	if !strings.Contains(p.What, path) || !strings.Contains(p.What, "not trusted") {
		t.Errorf("%s: What = %q; want it to name %s and say it is not trusted", label, p.What, path)
	}
	if !strings.Contains(p.Fix, "tell the human") || !strings.Contains(p.Fix, "yo-human config trust") || !strings.Contains(p.Fix, "their own terminal") {
		t.Errorf("%s: Fix = %q; want it to tell the human to run config trust in their own terminal", label, p.Fix)
	}
}

// A --config or YO_HUMAN_CONFIG file is chosen by the caller, who may be an agent. Outside the user
// config directory it counts only once the human trusts it, as a project file does: otherwise it
// could point a channel at any server with any of the human's secrets.
func TestExplicitFileOutsideTheUserDirNeedsTrust(t *testing.T) {
	w := newExplicitWorld(t)
	path := write(t, w.outside, "hostile.yaml", minimal)
	_, _, err := w.load(path, truster{})
	wantUntrustedExplicit(t, err, path, "untrusted")

	c, _, err := w.load(path, truster{path: sha(t, path)})
	if err != nil || len(c.Channels) != 1 {
		t.Fatalf("trusted explicit file: %v; want it loaded", err)
	}
	if err := os.WriteFile(path, []byte(minimal+"deadline: 2s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = w.load(path, truster{path: "stale"})
	wantUntrustedExplicit(t, err, path, "changed since trusted")
}

// A trusted explicit file outside the user directory obeys the other project rule too: it may not
// define a log channel, which would append agent text to any file it names.
func TestExplicitFileOutsideTheUserDirMayNotDefineALogChannel(t *testing.T) {
	w := newExplicitWorld(t)
	path := write(t, w.outside, "hostile.yaml", strings.Replace(withLog, "%s", filepath.Join(w.outside, "rc"), 1))
	_, _, err := w.load(path, truster{path: sha(t, path)})
	wantProjectLogRefused(t, err, "trusted explicit file outside the user dir")
}

// The user's own file, in their config directory, writable only by them, is theirs: no trust is
// needed and it may define a log channel.
func TestExplicitFileInTheUserDirIsTheUsersOwn(t *testing.T) {
	w := newExplicitWorld(t)
	path := write(t, w.userDir, "work.yaml", strings.Replace(withLog, "%s", filepath.Join(w.userDir, "y.log"), 1))
	c, _, err := w.load(path, truster{})
	if err != nil || len(c.Channels) != 2 {
		t.Fatalf("explicit file in the user dir: %v; want it loaded with its log channel", err)
	}
}

// A file in the user directory that someone else can write, or that sits in a directory someone
// else can write, is not the user's alone: it needs trust like any other.
func TestExplicitFileInTheUserDirWritableByOthersNeedsTrust(t *testing.T) {
	for _, tc := range []struct {
		name          string
		file, dirMode os.FileMode
	}{{"group-writable file", 0o620, 0o700}, {"world-writable file", 0o602, 0o700}, {"group-writable directory", 0o600, 0o770}} {
		w := newExplicitWorld(t)
		path := write(t, w.userDir, "work.yaml", minimal)
		if err := os.Chmod(path, tc.file); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(w.userDir, tc.dirMode); err != nil {
			t.Fatal(err)
		}
		_, _, err := w.load(path, truster{})
		wantUntrustedExplicit(t, err, path, tc.name)
	}
}

// A link in the user directory to a file outside it is the file outside: the resolved path decides.
func TestExplicitLinkFromTheUserDirToOutsideNeedsTrust(t *testing.T) {
	w := newExplicitWorld(t)
	target := write(t, w.outside, "hostile.yaml", minimal)
	link := filepath.Join(w.userDir, "work.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	_, _, err := w.load(link, truster{})
	wantUntrustedExplicit(t, err, target, "symlink out of the user dir")
}

// The notice for an untrusted project file is read by an agent: it must hand the decision to the
// human, never tell the agent to trust the file itself.
func TestUntrustedProjectNoticeTellsTheHuman(t *testing.T) {
	w := newExplicitWorld(t)
	write(t, w.cwd, ".yo-human.yaml", minimal)
	write(t, w.userDir, "config.yaml", minimal)
	_, notices, err := config.Load(config.Paths{Cwd: w.cwd, UserDir: w.userDir}, logRegistry(), truster{})
	if err != nil || len(notices) != 1 {
		t.Fatalf("Load: %v, notices %v; want one notice", err, notices)
	}
	text := notices[0].Text
	if !strings.Contains(text, "tell the human") || !strings.Contains(text, "their own terminal") || strings.Contains(text, "read it, then run") {
		t.Errorf("notice = %q; want it to tell the human to run config trust in their own terminal", text)
	}
}

// swapAt replaces the file at path with one holding body and mode, by rename, as an attacker racing
// the loader would.
func swapAt(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	tmp := path + ".swap"
	if err := os.WriteFile(tmp, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

// The bytes loaded and the file checked are one file: a group-writable file swapped for a clean one
// of the user's own between the read and the checks is still judged as the file that was read.
func TestExplicitFileCheckedIsTheFileRead(t *testing.T) {
	w := newExplicitWorld(t)
	path := write(t, w.userDir, "work.yaml", strings.Replace(minimal, "use_when: testing", "use_when: hostile", 1))
	if err := os.Chmod(path, 0o620); err != nil {
		t.Fatal(err)
	}
	defer config.SetExplicitGap(func() { swapAt(t, path, minimal, 0o600) })()
	c, _, err := w.load(path, truster{})
	if err == nil {
		t.Fatalf("loaded %q from a group-writable file swapped after the read; want it refused", c.Levels[0].UseWhen)
	}
	_ = wantProblem(t, err, problem.Config)
}

// A file of the user's own swapped for another after it was opened is refused: the path no longer
// names the file that was read.
func TestExplicitFileSwappedAfterTheOpenIsRefused(t *testing.T) {
	w := newExplicitWorld(t)
	path := write(t, w.userDir, "work.yaml", minimal)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	defer config.SetExplicitGap(func() { swapAt(t, path, minimal+"deadline: 9s\n", 0o600) })()
	_, _, err := w.load(path, truster{})
	if p := wantProblem(t, err, problem.Config); !strings.Contains(p.What, "changed while") {
		t.Errorf("What = %q; want it to say the file changed while it was read", p.What)
	}
}
