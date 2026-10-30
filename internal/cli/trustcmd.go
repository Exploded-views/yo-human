package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// absPath is a seam: a working directory that has been removed cannot be made to fail portably.
var absPath = filepath.Abs

// maxTrustBytes caps the project file, as config.Load does.
const maxTrustBytes = 1 << 20

// cmdTrust records a project config as trusted: its symlink-resolved absolute path (the key
// config.Load looks up) and the sha256 of the bytes read here. With no file it takes the nearest
// project file, as Load finds it. It shows what the file would do (every channel's type and
// non-secret settings, and where each level sends), so the human sees what they are trusting.
func cmdTrust(e *env, args []string) int {
	if len(args) > 1 {
		return e.fail(problem.Usagef("config trust takes at most one file", "yo-human config trust [<path to .yo-human.yaml or .yo-human.json>]",
			"read the file, then run `yo-human config trust ./.yo-human.yaml`"))
	}
	arg := ""
	if len(args) == 1 {
		arg = args[0]
	} else if arg = config.ProjectFile(e.paths().Cwd); arg == "" {
		return e.fail(problem.Usagef("no project config file was found from here", "a .yo-human.yaml or .yo-human.json in this directory or one above it",
			"run it where the project file is, or name the file: `yo-human config trust ./.yo-human.yaml`"))
	}
	path, data, err := readProjectFile(arg)
	if err != nil {
		return e.fail(err)
	}
	doc, err := trustable(path, data)
	if err != nil {
		return e.fail(err)
	}
	view := describe(doc, e.lowerLayers())
	lines := append([]string{"trusting " + strconv.Quote(path)}, view.lines()...)
	if err := approve(noninteractive(), "config trust", lines, "Trust this file?", map[string]any{"event": "trust", "path": path}); err != nil {
		return e.fail(err)
	}
	st, err := core.OpenState(stateDir())
	if err != nil {
		return e.fail(err)
	}
	sum := sha256.Sum256(data)
	if err := st.Trust(path, hex.EncodeToString(sum[:])); err != nil {
		return e.fail(err)
	}
	e.ok(append(lines, "trusted; a change to the file un-trusts it until you run this again"),
		map[string]any{"path": path, "sha256": hex.EncodeToString(sum[:]), "channels": view.Channels, "levels": view.Levels,
			"settings": view.Settings, "replaces": view.Replaces, "secrets": view.Secrets, "also_sets": view.AlsoSets})
	return ExitOK
}

// trustable parses a project file that may be trusted: a mapping with no control character, that
// the schema accepts (trusted, a file it refuses would fail every command).
func trustable(path string, data []byte) (map[string]any, error) {
	var doc map[string]any
	if yaml.Unmarshal(data, &doc) != nil {
		return nil, problem.Configf("%q is not a YAML or JSON mapping", "a mapping of settings; run `yo-human config schema` for the shape",
			"fix "+problem.ShellQuote(path)+" first", path)
	}
	if holdsControl(doc) {
		return nil, problem.Configf("%q holds a control character in a key or a value (it is not shown)", "keys and text without control characters, which could forge or erase lines on a terminal",
			"remove the control characters from "+problem.ShellQuote(path)+", then run `yo-human config trust` again", path)
	}
	return doc, config.CheckLayer(path, data)
}

// readProjectFile resolves arg to the key config.Load uses (absolute, symlinks resolved) and
// reads the file once, so the bytes that are shown and hashed are the bytes that were read.
func readProjectFile(arg string) (string, []byte, error) {
	abs, err := absPath(arg)
	if err != nil {
		return "", nil, problem.Usagef("cannot find the working directory to resolve the path", "an absolute path", "run from a directory that exists, or give the full path")
	}
	path, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", nil, problem.Usagef("there is no file at %q", "the path of a project config file", "check the path, and that the file exists", abs)
	}
	if trustOpenGap != nil {
		trustOpenGap()
	}
	f, _, err := config.OpenRegular(path) // never blocks on a FIFO swapped in after the path was resolved
	if config.IsNotRegular(err) {
		return "", nil, problem.Usagef("%q is not a regular file", "a regular YAML or JSON file", "name the file itself, not a directory or a device", path)
	}
	if err != nil {
		return "", nil, problem.Usagef("cannot open %q", "a readable regular file", "check that you can read "+problem.ShellQuote(path), path)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxTrustBytes+1))
	if err != nil || len(data) > maxTrustBytes {
		return "", nil, problem.Configf("cannot read %q, or it is larger than 1 MiB", "a YAML or JSON file of a few kilobytes", "shrink or fix "+problem.ShellQuote(path), path)
	}
	return path, data, nil
}

// holdsControl reports a control character (C0, DEL or C1) in any key of v, or any other than
// \n and \t in any string of v. Values are escaped or flattened when shown, so a block scalar and a
// tab are ordinary text; a key has no business holding either. A mapping with mixed key types is
// scanned like any other.
func holdsControl(v any) bool {
	if s, ok := v.(string); ok {
		return strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' })
	}
	keys, vals := children(v)
	for _, k := range keys {
		if s, _ := k.(string); strings.ContainsFunc(s, unicode.IsControl) {
			return true
		}
	}
	return slices.ContainsFunc(vals, holdsControl)
}

// children lists the keys and values directly inside a mapping or list.
func children(v any) (keys, vals []any) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			keys, vals = append(keys, k), append(vals, x)
		}
	case map[any]any:
		for k, x := range t {
			keys, vals = append(keys, k), append(vals, x)
		}
	case []any:
		vals = t
	}
	return keys, vals
}

// trustOpenGap is a test seam: it runs between resolving the path and opening it. Nil in the binary.
var trustOpenGap func()
