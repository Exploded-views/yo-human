//go:build unix

package secrets_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/Exploded-views/yo-human/internal/secrets"
)

// openSpy routes every open through fn and records the flags.
func openSpy(t *testing.T, fn func(name string) string) *[]int {
	t.Helper()
	flags := new([]int)
	t.Cleanup(secrets.SetOpenFile(func(name string, flag int, perm os.FileMode) (*os.File, error) {
		*flags = append(*flags, flag)
		return os.OpenFile(fn(name), flag, perm) //nolint:gosec // test paths
	}))
	return flags
}

// Something that is not a regular file is refused from its lstat alone: opening a device can
// have side effects.
func TestSecretsNonRegularNeverOpened(t *testing.T) {
	cases := map[string]func(t *testing.T, path string){
		"FIFO": func(t *testing.T, path string) {
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Skipf("mkfifo: %v", err)
			}
		},
		"directory": func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"symlink to a device": func(t *testing.T, path string) {
			if err := os.Symlink("/dev/null", path); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			path := privateFile(t)
			setup(t, path)
			flags := openSpy(t, func(n string) string { return n })
			_ = wantConfigProblem(t, func() error { _, err := secrets.Load(path); return err }())
			if len(*flags) != 0 {
				t.Errorf("the secrets path was opened %d times, want never", len(*flags))
			}
		})
	}
}

// The open cannot block, follow a symlink or acquire a controlling terminal, and the descriptor
// is checked again: a file swapped for a FIFO between the lstat and the open is refused.
func TestSecretsOpenFlagsAndDescriptorRecheck(t *testing.T) {
	path := privateFile(t)
	mustSet(t, path, "a", "VALUE-FAKE-ONE")
	swap := filepath.Join(filepath.Dir(path), "swap")
	if err := syscall.Mkfifo(swap, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	flags := openSpy(t, func(string) string { return swap })
	_, err := secrets.Load(path)
	p := wantConfigProblem(t, err)
	if !strings.Contains(p.What, "not a regular file") {
		t.Errorf("What = %q, want the swapped-in FIFO refused", p.What)
	}
	want := os.O_RDONLY | syscall.O_NONBLOCK | syscall.O_NOCTTY | syscall.O_NOFOLLOW
	if len(*flags) != 1 || (*flags)[0]&want != want || (*flags)[0]&(os.O_WRONLY|os.O_RDWR) != 0 {
		t.Errorf("open flags = %#x, want one read-only open with O_NONBLOCK|O_NOCTTY|O_NOFOLLOW", *flags)
	}
}

// When the open descriptor cannot be inspected, the real reason is reported.
func TestSecretsDescriptorStatFailureIsReported(t *testing.T) {
	path := privateFile(t)
	mustSet(t, path, "a", "VALUE-FAKE-ONE")
	t.Cleanup(secrets.SetStatOpen(func(f *os.File) (os.FileInfo, error) {
		return nil, &os.PathError{Op: "fstat", Path: f.Name(), Err: syscall.EIO}
	}))
	_, err := secrets.Load(path)
	p := wantConfigProblem(t, err)
	if !strings.Contains(p.What, syscall.EIO.Error()) || strings.Contains(p.What, "not a regular file") {
		t.Errorf("What = %q, want the real error", p.What)
	}
}

// The file can change between the lstat and the open; each outcome of the open is still handled.
func TestSecretsOpenRaces(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string // "" means an empty map and no error
	}{
		{"removed", syscall.ENOENT, ""},
		{"replaced by a symlink", syscall.ELOOP, "is a symlink"},
		{"made unreadable", syscall.EACCES, syscall.EACCES.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := privateFile(t)
			mustSet(t, path, "a", "VALUE-FAKE-ONE")
			t.Cleanup(secrets.SetOpenFile(func(name string, _ int, _ os.FileMode) (*os.File, error) {
				return nil, &os.PathError{Op: "open", Path: name, Err: tc.err}
			}))
			m, err := secrets.Load(path)
			if tc.want == "" {
				if err != nil || len(m) != 0 {
					t.Fatalf("Load = %v, %v; want an empty map", m, err)
				}
				return
			}
			if p := wantConfigProblem(t, err); !strings.Contains(p.What, tc.want) {
				t.Errorf("What = %q, want %q", p.What, tc.want)
			}
		})
	}
}

// A file that cannot even be lstat'ed is refused with the real reason.
func TestSecretsLstatFailureIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory modes")
	}
	path := privateFile(t)
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	_, err := secrets.Load(path)
	if p := wantConfigProblem(t, err); !strings.Contains(p.What, syscall.EACCES.Error()) {
		t.Errorf("What = %q, want the real error", p.What)
	}
}
