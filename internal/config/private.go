package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/Exploded-views/yo-human/internal/problem"
)

// usersOwn reports whether the opened file (fi, from its descriptor) and the directory holding it
// belong to the user and no one else can write either: only then is what it says the user's alone.
func usersOwn(fi os.FileInfo, dir string) bool {
	d, err := os.Stat(dir)
	return err == nil && private(fi) && private(d)
}

func private(fi os.FileInfo) bool { return isMine(fi) && fi.Mode().Perm()&0o022 == 0 }

// isMine is how ownership is checked; only tests replace it.
var isMine = ownedByMe

// within reports whether path lies inside dir, both symlink-resolved.
func within(path, dir string) bool {
	if dir == "" {
		return false
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(real, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolved is path made absolute with its symlinks resolved: the key a trust record uses.
func resolved(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

// trustedBytes asks trust whether the file at path, holding data, is trusted.
func trustedBytes(trust Truster, path string, data []byte) (bool, error) {
	if trust == nil {
		return false, nil
	}
	sum := sha256.Sum256(data)
	return trust.Trusted(path, hex.EncodeToString(sum[:]))
}

// explicitGap is a test seam: it runs between the open of the explicit file and its checks. Nil in
// the binary.
var explicitGap func()

// explicitKind says how the explicit file at real counts: as the user's own, or as trusted and held
// to the project rules. A file that is neither is refused.
func explicitKind(p Paths, trust Truster, real string, fi os.FileInfo, data []byte) (LayerKind, error) {
	if within(real, p.UserDir) && usersOwn(fi, filepath.Dir(real)) {
		return LayerExplicit, nil
	}
	ok, err := trustedBytes(trust, real, data)
	if err != nil || ok {
		return LayerExplicitOutside, err
	}
	return 0, problem.Configf("the config file %q, named by --config or YO_HUMAN_CONFIG, is not trusted, or has changed since it was",
		"a file in "+p.UserDir+" that only you can write, or a file the human has trusted",
		"stop and tell the human: they can read it and run `yo-human config trust "+problem.ShellQuote(real)+"` in their own terminal", real)
}

// readExplicit reads the --config or YO_HUMAN_CONFIG file. The caller chose it, and the caller may
// be an agent: only a file in the user config directory that is the user's alone counts as the
// user's own. Any other file obeys the project rules: it counts only once the human trusts its
// current content, and it may not define a log channel. The file is opened once; its bytes, mode
// and owner all come from that descriptor, and the path must still name it.
//
// The user config directory comes from XDG_CONFIG_HOME or HOME, which the caller sets. That is
// accepted: an agent allowed only to run yo-human cannot set the environment, and an agent with a
// whole shell can rewrite the real user config, or read the secrets file, without yo-human.
// Likewise a caller can adopt another project's trusted file (by --config, or a project file that
// links to it): the content is what the human trusted, so the impact is routing only.
func readExplicit(p Paths, trust Truster) (*layer, error) {
	f, fi, err := openRegular(p.Explicit)
	if err != nil {
		return nil, unreadable(p.Explicit, err, true)
	}
	defer func() { _ = f.Close() }()
	if explicitGap != nil {
		explicitGap()
	}
	data, err := readOpen(f)
	if err != nil {
		return nil, unreadable(p.Explicit, err, true)
	}
	real := resolved(p.Explicit)
	if now, err := os.Stat(real); err != nil || !os.SameFile(fi, now) {
		return nil, problem.Configf("the config file %q changed while it was read", "a file that stays put while yo-human reads it",
			"run the command again; if this keeps happening, stop and tell the human", real)
	}
	kind, err := explicitKind(p, trust, real, fi, data)
	if err != nil {
		return nil, err
	}
	l, err := parseLayer(p.Explicit, data)
	if l != nil {
		l.kind = kind
	}
	return l, err
}
