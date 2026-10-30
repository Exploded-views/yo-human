package cli

import (
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Exploded-views/yo-human/internal/secrets"
)

// lowerLayer is a config file the project file sits on top of.
type lowerLayer struct {
	label  string         // how a line names it: "system config", "user config", "--config file"
	closer bool           // the --config file wins over the project file: they only share names
	doc    map[string]any // the file as a mapping
}

// lowerLayers are the other config layers present: the system config, the user config, and a
// --config file in the user config directory.
func (e *env) lowerLayers() []lowerLayer {
	p := e.paths()
	var out []lowerLayer
	add := func(path, label string, closer bool) {
		if doc := readDoc(path); doc != nil {
			out = append(out, lowerLayer{label, closer, doc})
		}
	}
	add(p.System, "system config", false)
	add(filepath.Join(p.UserDir, "config.yaml"), "user config", false)
	if p.Explicit != "" && inDir(p.Explicit, p.UserDir) {
		add(p.Explicit, "--config file", true)
	}
	return out
}

// readDoc is the YAML or JSON file at path as a mapping, or nil.
func readDoc(path string) map[string]any {
	b, err := os.ReadFile(path) //nolint:gosec // a config layer path
	var doc map[string]any
	if err != nil || yaml.Unmarshal(b, &doc) != nil {
		return nil
	}
	return doc
}

// inDir reports whether path, symlinks resolved, lies inside dir.
func inDir(path, dir string) bool {
	real, err1 := filepath.EvalSymlinks(path)
	d, err2 := filepath.EvalSymlinks(dir)
	rel, err3 := filepath.Rel(d, real)
	return err1 == nil && err2 == nil && err3 == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// replaces lists what the project file overrides in each lower layer: a channel of the same name,
// and the whole ladder. A closer layer (the --config file) wins, so they only share a name.
func replaces(doc map[string]any, lower []lowerLayer) []string {
	out := []string{}
	chans, _ := doc["channels"].(map[string]any)
	_, ladder := doc["levels"]
	for _, l := range lower {
		theirs, _ := l.doc["channels"].(map[string]any)
		for _, name := range sortedNames(chans) {
			switch _, ok := theirs[name]; {
			case ok && l.closer:
				out = append(out, `channel "`+esc(name)+`" shares its name with a channel in your `+l.label)
			case ok:
				out = append(out, `channel "`+esc(name)+`" REPLACES the channel of that name in your `+l.label)
			}
		}
		if ladder && l.doc["levels"] != nil && !l.closer {
			out = append(out, "levels REPLACE the whole ladder of your "+l.label)
		}
	}
	return out
}

// secretRoutes says, for each secret a project channel uses, where it sends it, and where the
// human's own config sends that secret when that is somewhere else.
func secretRoutes(chans map[string]any, lower []lowerLayer) []string {
	theirs := map[string]string{}
	for _, l := range lower {
		lc, _ := l.doc["channels"].(map[string]any)
		for _, name := range sortedNames(lc) {
			for secret, dest := range routesOf(lc[name]) {
				theirs[secret] = dest
			}
		}
	}
	out := []string{}
	for _, name := range sortedNames(chans) {
		routes := routesOf(chans[name])
		for _, secret := range sortedNames(routes) {
			line := `channel "` + esc(name) + `" sends secret ` + strconv.Quote(secret) + " to " + routes[secret]
			if t, ok := theirs[secret]; ok && t != routes[secret] {
				line += " (your config sends it to " + t + ")"
			}
			out = append(out, line)
		}
	}
	return out
}

// routesOf maps each secret a channel references to "<type> at <host>".
func routesOf(raw any) map[string]string {
	settings, _ := raw.(map[string]any)
	typ, _ := settings["type"].(string)
	dest := esc(typ) + " at " + hostOf(settings)
	out := map[string]string{}
	for _, v := range settings {
		ref, _ := v.(map[string]any)
		if name, _ := ref["secret"].(string); len(ref) == 1 && secrets.ValidName(name) {
			out[name] = dest
		}
	}
	return out
}

// hostOf is the host a channel's server or URL setting names, as the DNS sees it, or "its default
// host" when it sets none.
func hostOf(settings map[string]any) string {
	for _, key := range sortedNames(settings) {
		s, ok := settings[key].(string)
		if !ok || (key != "server" && !strings.HasSuffix(key, "url")) {
			continue
		}
		if u, err := url.Parse(s); err == nil && u.Host != "" {
			host, _ := asciiHost(u.Host)
			return esc(host)
		}
	}
	return "its default host"
}
