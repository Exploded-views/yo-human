package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// Without an absolute HOME (and no absolute XDG directories in its place) yo-human has nowhere
// safe to keep its config and state: it exits 78 and creates nothing, rather than writing them
// relative to the working directory.
func TestNoAbsoluteHomeIsAConfigProblemAndCreatesNothing(t *testing.T) {
	for _, home := range []string{"", "relative/home", "."} {
		s := newSandbox(t)
		t.Setenv("HOME", home)
		cwd, _ := os.Getwd()
		for _, args := range [][]string{{"levels"}, {"status"}, {"notify", "--level", "page", "--title", "t", "--ask", "a"}, {"config", "init"}} {
			r := s.do("", args...)
			expect(t, r.code == ExitConfig, "HOME=%q %v: %+v; want exit 78", home, args, r)
			mustContain(t, "stderr", r.err, "HOME")
		}
		ents, _ := os.ReadDir(cwd)
		expect(t, len(ents) == 0, "HOME=%q: the working directory now holds %v", home, ents)
		expect(t, s.do("", "version").code == ExitOK, "version needs no HOME")
	}
}

// A relative XDG_CONFIG_HOME or XDG_STATE_HOME is ignored, as the XDG specification says: the
// HOME fallbacks are used, and nothing lands in the working directory.
func TestRelativeXDGDirectoriesAreIgnored(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	t.Setenv("XDG_CONFIG_HOME", "cfg")
	t.Setenv("XDG_STATE_HOME", "state")
	cwd, _ := os.Getwd()
	r := s.do("", "levels")
	expect(t, r.code == ExitOK, "levels: %+v; want the user config under HOME read", r)
	s.do("", "status")
	_, err := os.Stat(filepath.Join(s.home, ".local", "state", "yo-human"))
	expect(t, err == nil, "the state is not under HOME: %v", err)
	ents, _ := os.ReadDir(cwd)
	expect(t, len(ents) == 0, "the working directory now holds %v", ents)
}

// With absolute XDG directories, HOME is not needed.
func TestAbsoluteXDGDirectoriesNeedNoHome(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(s.home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(s.home, ".local", "state"))
	t.Setenv("HOME", "")
	r := s.do("", "levels")
	expect(t, r.code == ExitOK, "levels with XDG set and no HOME: %+v", r)
}
