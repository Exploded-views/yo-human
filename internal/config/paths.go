package config

import (
	"os"
	"path/filepath"
)

// Paths says where each layer lives. Explicit and System are files; UserDir holds config.yaml (and
// secrets.yaml); Cwd is where the search for a project file starts, and Home where it stops.
type Paths struct{ Explicit, Cwd, UserDir, System, Home string }

const systemFile = "/etc/yo-human/config.yaml"

// DefaultPaths builds the paths from YO_HUMAN_CONFIG and the XDG variables, falling back to
// $HOME/.config as the XDG specification says.
func DefaultPaths(cwd string) Paths {
	return Paths{
		Explicit: os.Getenv("YO_HUMAN_CONFIG"),
		Cwd:      cwd,
		UserDir:  filepath.Join(XDGDir("XDG_CONFIG_HOME", ".config"), "yo-human"),
		System:   systemFile,
		Home:     absHome(),
	}
}

// XDGDir is the XDG base directory env names, or HOME/fallback. A relative value is ignored, as the
// XDG specification says. The caller has checked (HomeProblem) that the result is absolute.
func XDGDir(env, fallback string) string {
	if v := os.Getenv(env); filepath.IsAbs(v) {
		return v
	}
	return filepath.Join(os.Getenv("HOME"), fallback)
}

// absHome is HOME when it is absolute, else "".
func absHome() string {
	if h := os.Getenv("HOME"); filepath.IsAbs(h) {
		return h
	}
	return ""
}

// projectFile finds .yo-human.yaml or .yo-human.json in dir or its nearest ancestor that has one,
// whatever its kind (a directory, FIFO or dangling symlink stops the search too, and is then
// ignored with a notice, so a file farther up never applies in its place), and returns its
// symlink-resolved absolute path, which is the key the trust record uses. The search never looks
// above home, nor in or above a directory the user does not own (a shared /tmp, another user's
// checkout). It returns "" when there is none.
func projectFile(dir, home string) string {
	path, _ := projectSearch(dir, home)
	return path
}

// projectSearch is projectFile, also returning the real directory in which the search found the
// entry: an ancestor of dir (or dir), never where a symlinked project file points.
func projectSearch(dir, home string) (path, found string) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", ""
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	homeInfo, _ := os.Stat(home)
	for {
		if fi, err := os.Stat(dir); err != nil || !isMine(fi) {
			return "", ""
		}
		if p := projectIn(dir); p != "" {
			return p, dir
		}
		parent := filepath.Dir(dir)
		if parent == dir || (homeInfo != nil && sameDir(dir, homeInfo)) {
			return "", ""
		}
		dir = parent
	}
}

// projectIn is the project file in dir, symlink-resolved, or "".
func projectIn(dir string) string {
	for _, name := range []string{".yo-human.yaml", ".yo-human.json"} {
		p := filepath.Join(dir, name)
		if _, err := os.Lstat(p); err == nil { // anything by that name stops the search; an unusable one gets a notice
			if real, err := filepath.EvalSymlinks(p); err == nil {
				return real
			}
			return p
		}
	}
	return ""
}

func sameDir(dir string, fi os.FileInfo) bool {
	d, err := os.Stat(dir)
	return err == nil && os.SameFile(d, fi)
}

// ProjectFile is the nearest project file from dir, symlink-resolved, the search stopping at HOME:
// the file Load would read, and the key a trust record uses. It is "" when there is none.
func ProjectFile(dir string) string { return projectFile(dir, absHome()) }

// ProjectRoot is the real directory in which the search from dir found a project file (an ancestor
// of dir, never a symlinked file's target directory), or "" when there is none.
func ProjectRoot(dir string) string {
	_, found := projectSearch(dir, absHome())
	return found
}

// ProjectFileFrom is ProjectFile with the search stopping at home.
func ProjectFileFrom(dir, home string) string { return projectFile(dir, home) }
