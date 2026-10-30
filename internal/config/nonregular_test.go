//go:build unix

package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/problem"
)

func mkfifo(t *testing.T, path string) string {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	return path
}

func mkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// opensOf counts how often Load opens target, and records the flags of every open.
func opensOf(t *testing.T, target string) (count *int, flags *[]int) {
	t.Helper()
	count, flags = new(int), new([]int)
	restore := config.SetOpenFile(func(name string, flag int, perm os.FileMode) (*os.File, error) {
		*flags = append(*flags, flag)
		if name == target {
			*count++
		}
		return os.OpenFile(name, flag, perm) //nolint:gosec // test paths
	})
	t.Cleanup(restore)
	return count, flags
}

type nonRegularCase struct {
	name   string
	setup  func(t *testing.T, dir string) (config.Paths, string)
	notice bool
}

func nonRegularCases() []nonRegularCase {
	none := func(dir string) string { return filepath.Join(dir, "none") }
	return []nonRegularCase{
		{"project FIFO", func(t *testing.T, dir string) (config.Paths, string) {
			return userOnly(t, dir), mkfifo(t, filepath.Join(dir, ".yo-human.yaml"))
		}, true},
		{"project directory", func(t *testing.T, dir string) (config.Paths, string) {
			return userOnly(t, dir), mkdir(t, filepath.Join(dir, ".yo-human.yaml"))
		}, true},
		{"user FIFO", func(t *testing.T, dir string) (config.Paths, string) {
			u := mkdir(t, filepath.Join(dir, "u"))
			return config.Paths{Cwd: none(dir), UserDir: u}, mkfifo(t, filepath.Join(u, "config.yaml"))
		}, false},
		{"system FIFO", func(t *testing.T, dir string) (config.Paths, string) {
			f := mkfifo(t, filepath.Join(dir, "system.yaml"))
			return config.Paths{Cwd: none(dir), System: f}, f
		}, false},
		{"system device", func(_ *testing.T, dir string) (config.Paths, string) {
			return config.Paths{Cwd: none(dir), System: "/dev/null"}, "/dev/null"
		}, false},
		{"explicit FIFO", func(t *testing.T, dir string) (config.Paths, string) {
			f := mkfifo(t, filepath.Join(dir, "c.yaml"))
			return config.Paths{Cwd: none(dir), Explicit: f}, f
		}, false},
		{"explicit device", func(_ *testing.T, dir string) (config.Paths, string) {
			return config.Paths{Cwd: none(dir), Explicit: "/dev/null"}, "/dev/null"
		}, false},
	}
}

// Opening a device can have side effects, so a file that is not regular is refused from its stat
// alone and never opened.
func TestNonRegularConfigFilesAreNeverOpened(t *testing.T) {
	for _, tc := range nonRegularCases() {
		t.Run(tc.name, func(t *testing.T) {
			p, target := tc.setup(t, canon(t, t.TempDir()))
			opens, _ := opensOf(t, target)
			r := loadWithin(t, p, truster{})
			if *opens != 0 {
				t.Errorf("%s was opened %d times, want never", target, *opens)
			}
			if tc.notice {
				if r.err != nil {
					t.Fatalf("Load: %v, want the project file ignored", r.err)
				}
				oneNotice(t, r.notices, "not a regular file")
				return
			}
			pr := wantProblem(t, r.err, problem.Config)
			if !strings.Contains(pr.What, "not a regular file") {
				t.Errorf("What = %q", pr.What)
			}
		})
	}
}

func TestNonRegularFixSuitsTheLayer(t *testing.T) {
	for _, tc := range nonRegularCases() {
		if tc.notice {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			p, target := tc.setup(t, canon(t, t.TempDir()))
			pr := wantProblem(t, loadWithin(t, p, truster{}).err, problem.Config)
			if !strings.Contains(pr.What, `"`+target+`"`) {
				t.Errorf("What = %q, want it to name %q", pr.What, target)
			}
			if strings.Contains(pr.Fix, "rm ") || strings.Contains(pr.Fix, "remove") {
				t.Errorf("Fix = %q, want no removal suggested", pr.Fix)
			}
			want := "replace " + problem.ShellQuote(target) + " with a regular file"
			if p.Explicit != "" {
				want = "point --config or YO_HUMAN_CONFIG at a regular file"
			} else if strings.Contains(pr.Fix, "--config") {
				t.Errorf("Fix = %q, want no --config for the %s", pr.Fix, tc.name)
			}
			if !strings.Contains(pr.Fix, want) {
				t.Errorf("Fix = %q, want it to contain %q", pr.Fix, want)
			}
		})
	}
}

// A candidate that cannot be used still stops the search: a file farther up must not apply in
// its place.
func TestProjectSearchStopsAtNearestCandidate(t *testing.T) {
	cases := []struct {
		name   string
		block  func(t *testing.T, path string)
		notice string
	}{
		{"no blocker", func(*testing.T, string) {}, ""},
		{"dangling symlink", func(t *testing.T, path string) {
			if err := os.Symlink(filepath.Join(filepath.Dir(path), "missing"), path); err != nil {
				t.Fatal(err)
			}
		}, "does not exist"},
		{"directory", func(t *testing.T, path string) { mkdir(t, path) }, "not a regular file"},
		{"FIFO", func(t *testing.T, path string) { mkfifo(t, path) }, "not a regular file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := canon(t, t.TempDir())
			far := write(t, a, ".yo-human.yaml", "deadline: 9s\n")
			cwd := mkdir(t, filepath.Join(a, "b"))
			tc.block(t, filepath.Join(cwd, ".yo-human.yaml"))
			r := loadWithin(t, userOnly(t, cwd), truster{far: sha(t, far)})
			if r.err != nil {
				t.Fatalf("Load: %v", r.err)
			}
			c := r.cfg
			if tc.notice == "" {
				if len(r.notices) != 0 || c.Deadline != 9*time.Second {
					t.Fatalf("notices = %+v, Deadline = %v; want the far file used", r.notices, c.Deadline)
				}
				return
			}
			oneNotice(t, r.notices, tc.notice)
			if c.Deadline != 3*time.Second {
				t.Errorf("Deadline = %v, want 3s from the user file: the search went past the nearest candidate", c.Deadline)
			}
		})
	}
}

// Every config open is non-blocking and cannot acquire a controlling terminal, and what was
// opened is checked again: a file swapped for a FIFO between the stat and the open is refused.
func TestConfigOpenFlagsAndDescriptorRecheck(t *testing.T) {
	dir := canon(t, t.TempDir())
	f := write(t, dir, "c.yaml", minimal)
	swap := mkfifo(t, filepath.Join(dir, "swap"))
	var flags []int
	t.Cleanup(config.SetOpenFile(func(_ string, flag int, perm os.FileMode) (*os.File, error) {
		flags = append(flags, flag)
		return os.OpenFile(swap, flag, perm) //nolint:gosec // test paths
	}))
	r := loadWithin(t, config.Paths{Cwd: filepath.Join(dir, "none"), Explicit: f}, truster{})
	pr := wantProblem(t, r.err, problem.Config)
	if !strings.Contains(pr.What, "not a regular file") {
		t.Errorf("What = %q, want the swapped-in FIFO refused", pr.What)
	}
	want := os.O_RDONLY | syscall.O_NONBLOCK | syscall.O_NOCTTY
	for _, fl := range flags {
		if fl&want != want || fl&(os.O_WRONLY|os.O_RDWR) != 0 {
			t.Errorf("open flags = %#x, want O_RDONLY|O_NONBLOCK|O_NOCTTY", fl)
		}
	}
	if len(flags) == 0 {
		t.Error("no open went through the seam")
	}
}

// When the open descriptor cannot be inspected, the real reason is reported.
func TestConfigDescriptorStatFailureIsReported(t *testing.T) {
	t.Cleanup(config.SetStatOpen(func(f *os.File) (os.FileInfo, error) {
		return nil, &os.PathError{Op: "fstat", Path: f.Name(), Err: syscall.EIO}
	}))
	dir := canon(t, t.TempDir())
	r := loadWithin(t, config.Paths{Cwd: filepath.Join(dir, "none"), Explicit: write(t, dir, "c.yaml", minimal)}, truster{})
	pr := wantProblem(t, r.err, problem.Config)
	if !strings.Contains(pr.What, syscall.EIO.Error()) || strings.Contains(pr.What, "not a regular file") {
		t.Errorf("What = %q, want the real error", pr.What)
	}
	write(t, dir, ".yo-human.yaml", minimal)
	r = loadWithin(t, config.Paths{Cwd: dir}, truster{}) // no other layer, so the load fails after the notice
	text := oneNotice(t, r.notices, syscall.EIO.Error())
	if strings.Contains(text, "not a regular file") {
		t.Errorf("notice = %q, want the real error", text)
	}
}
