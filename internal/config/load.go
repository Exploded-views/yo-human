package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// Truster says whether the project file at path, whose content hashes to sha, is trusted.
// *store.Store satisfies it.
type Truster interface {
	Trusted(path, sha string) (bool, error)
}

// Notice is a line for stderr that is not an error.
type Notice struct {
	Text    string
	Problem *problem.Problem
}

// layer is one parsed config file.
type layer struct {
	path string
	kind LayerKind
	data map[string]any
}

var (
	schemaOnce sync.Once
	layerSchem *schemaNode
	schemaErr  error
)

func layerSchema() (*schemaNode, error) {
	schemaOnce.Do(func() { layerSchem, schemaErr = compileNode(schemaJSON) })
	return layerSchem, schemaErr
}

// maxBytes caps every config read; a config file is a few kilobytes.
const maxBytes = 1 << 20

// Load reads the layers, closest last: system, user, the nearest project file (only if trusted),
// then the explicit file. It merges them with the closest winning, applies defaults and runs
// Check: every error-kind finding fails the load (joined), and warnings come back as Notices. An
// untrusted, unreadable, oversize or changed project file is skipped and reported as a Notice.
// Notices come back alongside an error too.
func Load(p Paths, reg channel.Registry, trust Truster) (*Config, []Notice, error) {
	layers, notices, err := readLayers(p, trust)
	if err != nil {
		return nil, notices, err
	}
	if len(layers) == 0 {
		return nil, notices, problem.Configf("no config file was found", fmt.Sprintf("a file at --config or YO_HUMAN_CONFIG, a trusted .yo-human.yaml, %q or %q",
			filepath.Join(p.UserDir, "config.yaml"), p.System), "run `yo-human config init` to write a starter config")
	}
	merged, origin, kinds := merge(layers)
	c, err := decode(merged, origin)
	if err != nil {
		return nil, notices, err
	}
	c.OriginLayer, c.Merged = kinds, merged
	var faults []error
	for _, f := range Check(c, reg) {
		if IsWarning(f) {
			notices = append(notices, Notice{Text: fmt.Sprintf("%s (%s)", f.What, f.Fix), Problem: f})
		} else {
			faults = append(faults, f)
		}
	}
	if len(faults) > 0 {
		return nil, notices, errors.Join(faults...)
	}
	return c, notices, nil
}

// readLayers reads every layer that exists, farthest first.
func readLayers(p Paths, trust Truster) ([]layer, []Notice, error) {
	var layers []layer
	var notices []Notice
	user := ""
	if p.UserDir != "" {
		user = filepath.Join(p.UserDir, "config.yaml")
	}
	for _, f := range []struct {
		path string
		kind LayerKind
	}{{p.System, LayerSystem}, {user, LayerUser}} {
		l, err := readOptional(f.path)
		if err != nil {
			return nil, notices, err
		}
		layers = appendLayer(layers, l, f.kind)
	}
	l, n, err := readProject(p.Cwd, p.Home, trust)
	if err != nil {
		return nil, notices, err
	}
	layers = appendLayer(layers, l, LayerProject)
	if n != nil {
		notices = append(notices, *n)
	}
	if p.Explicit != "" {
		l, err := readExplicit(p, trust)
		if err != nil {
			return nil, notices, err
		}
		layers = append(layers, *l)
	}
	return layers, notices, nil
}

func appendLayer(layers []layer, l *layer, kind LayerKind) []layer {
	if l == nil {
		return layers
	}
	l.kind = kind
	return append(layers, *l)
}

func readOptional(path string) (*layer, error) {
	if path == "" {
		return nil, nil
	}
	data, err := readCapped(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, unreadable(path, err, false)
	}
	return parseLayer(path, data)
}

// unreadable is the problem for a system, user or explicit layer that cannot be read. Only the
// explicit layer came from --config or YO_HUMAN_CONFIG, so only its fix points there.
func unreadable(path string, err error, explicit bool) error {
	if errors.Is(err, errTooBig) {
		return problem.Configf("the config file %q is larger than 1 MiB", "a YAML or JSON file of a few kilobytes",
			"shrink "+problem.ShellQuote(path), path)
	}
	if errors.Is(err, errNotRegular) {
		fix := "replace " + problem.ShellQuote(path) + " with a regular file holding the YAML or JSON config"
		if explicit {
			fix = "point --config or YO_HUMAN_CONFIG at a regular file"
		}
		return problem.Configf("the config file %q is not a regular file", "a YAML or JSON file", fix, path)
	}
	return problem.Configf("cannot read the config file %q: %s", "a readable YAML or JSON file",
		"check that "+problem.ShellQuote(path)+" exists and that you can read it; the path comes from --config, YO_HUMAN_CONFIG or the search path", path, reason(err))
}

// readProject returns the nearest project file if the human trusts its current content. A file it
// cannot read, or that is too large, is ignored with a Notice rather than failing every command.
// The bytes that are hashed are the bytes that are parsed, so a file cannot change between check
// and use, and the path that is hashed and looked up is the symlink-resolved one.
func readProject(cwd, home string, trust Truster) (*layer, *Notice, error) {
	if cwd == "" {
		return nil, nil, nil
	}
	path := projectFile(cwd, home)
	if path == "" {
		return nil, nil, nil
	}
	data, n := readOwnProject(path)
	if n != nil {
		return nil, n, nil
	}
	ok, err := trustedBytes(trust, path, data)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, &Notice{Text: fmt.Sprintf("ignoring the project config %q: it is not trusted, or has changed since it was; "+
			"tell the human: they can read it and run `yo-human config trust %s` in their own terminal", path, problem.ShellQuote(path))}, nil
	}
	l, err := parseLayer(path, data)
	return l, nil, err
}

// readOwnProject reads the project file through one descriptor, and only if it and its directory
// are the user's alone: a file another user owns, or that others can write, is not the human's to
// trust. Otherwise it returns the notice saying why the file is ignored.
func readOwnProject(path string) ([]byte, *Notice) {
	f, fi, err := openRegular(path)
	if err != nil {
		return nil, projectNotice(path, err)
	}
	defer func() { _ = f.Close() }()
	switch {
	case !isMine(fi):
		return nil, &Notice{Text: fmt.Sprintf("ignoring the project config %q: it belongs to another user", path)}
	case !usersOwn(fi, filepath.Dir(path)):
		return nil, &Notice{Text: fmt.Sprintf("ignoring the project config %q: someone other than you can write it or its directory; "+
			"tell the human: they can make it theirs alone (chmod go-w on the file and its directory)", path)}
	}
	data, err := readOpen(f)
	if err != nil {
		return nil, projectNotice(path, err)
	}
	return data, nil
}

// projectNotice says why the project file at path is ignored.
func projectNotice(path string, err error) *Notice {
	why := ""
	switch {
	case errors.Is(err, errTooBig):
		why = "it is larger than 1 MiB"
	case errors.Is(err, errNotRegular):
		why = "it is not a regular file"
	case errors.Is(err, fs.ErrNotExist): // the search saw it, so it is a symlink to nothing
		why = "it is a symlink whose target does not exist"
	default:
		return &Notice{Text: fmt.Sprintf("cannot read the project config %q (%s); ignoring it", path, reason(err))}
	}
	return &Notice{Text: fmt.Sprintf("ignoring the project config %q: %s", path, why)}
}

// yamlLine is compiled on first use, not at init: init runs on every command, version included.
var yamlLine = sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile(`line (\d+)`) })

// parseLayer parses one file and validates it against the schema. Neither the parser's message
// nor the schema's quotes file content: a mistaken secret must not be echoed.
func parseLayer(path string, data []byte) (*layer, error) {
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		where := ""
		if m := yamlLine().FindStringSubmatch(err.Error()); m != nil {
			where = " at line " + m[1]
		}
		return nil, problem.Configf("%q is not valid YAML or JSON%s", "a YAML or JSON mapping; run `yo-human config schema` for the shape",
			"fix the syntax in "+problem.ShellQuote(path), path, where)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	m, ok := doc.(map[string]any)
	if !ok {
		return nil, problem.Configf("%q must be a mapping of settings with text keys", "a YAML or JSON mapping; run `yo-human config schema` for the shape",
			"make the top level of "+problem.ShellQuote(path)+" a mapping", path)
	}
	sch, err := layerSchema()
	if err != nil {
		return nil, problem.Internalf("the embedded config schema does not compile", "a valid JSON Schema", "report this as a bug")
	}
	found, err := nodeViolations(sch, m)
	if err != nil {
		return nil, problem.Configf("%q uses a structure JSON cannot express", "a YAML or JSON mapping; run `yo-human config schema` for the shape",
			"use text keys and plain values in "+problem.ShellQuote(path), path)
	}
	if len(found) > 0 {
		return nil, problem.Configf("%q breaks the config schema: %s", "run `yo-human config schema` for the accepted shape",
			"edit "+problem.ShellQuote(path)+" so each setting listed matches the schema", path, summary(found))
	}
	return &layer{path: path, data: m}, nil
}

// summary lists the first three violations.
func summary(found []violation) string {
	var parts []string
	for i, v := range found {
		if i == 3 {
			parts = append(parts, fmt.Sprintf("and %d more", len(found)-3))
			break
		}
		parts = append(parts, v.String())
	}
	return strings.Join(parts, "; ")
}
