package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/config"
)

// A project file someone else can write is not the human's to trust: it is ignored with a notice,
// whatever the trust record says, and so is one in a directory someone else can write.
func TestProjectFileWritableByOthersIsIgnored(t *testing.T) {
	for _, tc := range []struct {
		name          string
		file, dirMode os.FileMode
	}{{"group-writable file", 0o664, 0o755}, {"world-writable file", 0o646, 0o755}, {"group-writable directory", 0o644, 0o775}, {"world-writable directory", 0o644, 0o757}} {
		root := canon(t, t.TempDir())
		proj := write(t, root, ".yo-human.yaml", "deadline: 9s\n")
		if err := os.Chmod(proj, tc.file); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(root, tc.dirMode); err != nil {
			t.Fatal(err)
		}
		c, notices, err := config.Load(config.Paths{Cwd: root, UserDir: abs(t, "testdata/user")}, registry(), truster{proj: sha(t, proj)})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if c.Origin["deadline"] == proj || len(notices) != 1 || !strings.Contains(notices[0].Text, "can write") {
			t.Errorf("%s: deadline from %q, notices %+v; want the file ignored with a notice that others can write it", tc.name, c.Origin["deadline"], notices)
		}
	}
}

// A project file another user owns is ignored, trusted or not.
func TestProjectFileOfAnotherUserIsIgnored(t *testing.T) {
	root := canon(t, t.TempDir())
	proj := write(t, root, ".yo-human.yaml", "deadline: 9s\n")
	real, _ := os.Stat(proj)
	defer config.SetMine(func(fi os.FileInfo) bool { return !os.SameFile(fi, real) })()
	c, notices, err := config.Load(config.Paths{Cwd: root, UserDir: abs(t, "testdata/user")}, registry(), truster{proj: sha(t, proj)})
	if err != nil {
		t.Fatal(err)
	}
	if c.Origin["deadline"] == proj || len(notices) != 1 || !strings.Contains(notices[0].Text, "another user") {
		t.Errorf("deadline from %q, notices %+v; want the file ignored with a notice that another user owns it", c.Origin["deadline"], notices)
	}
}

// The search for a project file stops at $HOME: a file above the home directory (in a shared
// directory, say) is never found.
func TestProjectSearchStopsAtHome(t *testing.T) {
	root := canon(t, t.TempDir())
	write(t, root, ".yo-human.yaml", "deadline: 9s\n")
	home := filepath.Join(root, "home")
	cwd := filepath.Join(home, "work")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := config.ProjectFileFrom(cwd, home); got != "" {
		t.Errorf("found %q above $HOME", got)
	}
	inHome := write(t, home, ".yo-human.yaml", "deadline: 8s\n")
	if got := config.ProjectFileFrom(cwd, home); got != inHome {
		t.Errorf("found %q, want the file in $HOME itself, %q", got, inHome)
	}
}

// The search stops at a directory the user does not own (such as /tmp, or a shared checkout's
// parent): a file there, or above it, is never found.
func TestProjectSearchStopsAtADirectoryOfAnotherUser(t *testing.T) {
	root := canon(t, t.TempDir())
	write(t, root, ".yo-human.yaml", "deadline: 9s\n")
	shared := filepath.Join(root, "shared")
	cwd := filepath.Join(shared, "mine")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, shared, ".yo-human.yaml", "deadline: 7s\n")
	sh, _ := os.Stat(shared)
	defer config.SetMine(func(fi os.FileInfo) bool { return !os.SameFile(fi, sh) })()
	if got := config.ProjectFileFrom(cwd, ""); got != "" {
		t.Errorf("found %q at or above a directory another user owns", got)
	}
}
