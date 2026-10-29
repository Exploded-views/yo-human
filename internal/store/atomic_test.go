package store

import (
	"errors"
	"io/fs"
	"syscall"

	"github.com/Exploded-views/yo-human/internal/problem"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicLeavesNoTempOnSuccess(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.json")
	if err := WriteFileAtomic(target, []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name() != "a.json" {
		t.Fatalf("directory holds %v, want only a.json", ents)
	}
	fi, _ := os.Stat(target)
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("perm = %o, want 640", fi.Mode().Perm())
	}
	if b, _ := os.ReadFile(target); string(b) != "hello" {
		t.Errorf("content = %q", b)
	}
}

func TestWriteFileAtomicReplacesExisting(t *testing.T) {
	target := filepath.Join(t.TempDir(), "a")
	for _, c := range []string{"one", "two"} {
		if err := WriteFileAtomic(target, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if b, _ := os.ReadFile(target); string(b) != "two" {
		t.Errorf("content = %q, want two", b)
	}
}

func TestWriteFileAtomicFailureLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	// The target is a non-empty directory, so the final rename fails after the temp is written.
	target := filepath.Join(dir, "t")
	if err := os.MkdirAll(filepath.Join(target, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(target, []byte("x"), 0o600); err == nil {
		t.Fatal("want an error renaming over a directory")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Errorf("directory holds %v, want only the target directory", ents)
	}
}

func TestWriteFileAtomicMissingDirectory(t *testing.T) {
	if err := WriteFileAtomic(filepath.Join(t.TempDir(), "no", "f"), nil, 0o600); err == nil {
		t.Fatal("want an error for a missing directory")
	}
}

func TestWriteFileExclusiveCreatesOnceAndNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "c.yaml")
	if err := WriteFileExclusive(target, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(target); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("stat %v, %v; want mode 0600", fi, err)
	}
	err := WriteFileExclusive(target, []byte("second"), 0o600)
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("second write err = %v, want fs.ErrExist", err)
	}
	if b, _ := os.ReadFile(target); string(b) != "first" {
		t.Errorf("content = %q, want the first write untouched", b)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Errorf("directory holds %v, want only c.yaml (no temp file left)", ents)
	}
}

func TestWriteFileExclusiveReportsAStoreProblemWhereItCannotWrite(t *testing.T) {
	err := WriteFileExclusive(filepath.Join(t.TempDir(), "missing", "c.yaml"), []byte("x"), 0o600)
	var p *problem.Problem
	if !errors.As(err, &p) || p.Kind != problem.Store {
		t.Errorf("err = %v, want a Store problem", err)
	}
}

// Where the filesystem cannot hard-link, the file is created with O_EXCL, still never replacing.
func TestWriteFileExclusiveFallsBackWithoutHardLinks(t *testing.T) {
	called := false
	old := linkFile
	linkFile = func(string, string) error { called = true; return syscall.ENOTSUP }
	t.Cleanup(func() { linkFile = old })
	dir := t.TempDir()
	target := filepath.Join(dir, "c.yaml")
	if err := WriteFileExclusive(target, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("the hard link was never tried")
	}
	if b, _ := os.ReadFile(target); string(b) != "first" {
		t.Errorf("content = %q", b)
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	if err := WriteFileExclusive(target, []byte("second"), 0o600); !errors.Is(err, fs.ErrExist) {
		t.Errorf("second write err = %v, want fs.ErrExist", err)
	}
	if b, _ := os.ReadFile(target); string(b) != "first" {
		t.Errorf("content after the second write = %q", b)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Errorf("directory holds %v, want only c.yaml", ents)
	}
	if err := WriteFileExclusive(filepath.Join(dir, "no", "such", "c.yaml"), []byte("x"), 0o600); err == nil {
		t.Error("write into a missing directory succeeded")
	}
}
