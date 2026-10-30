package config

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"

	"github.com/Exploded-views/yo-human/internal/channel/registry"
)

// schemaPair is one schema the binary validates against, compiled by both validators.
type schemaPair struct {
	name string
	ref  *jsonschema.Schema
	own  *schemaNode
}

// schemaPairs is the config schema, every channel type's settings schema, and keywordSchema.
func schemaPairs(t testing.TB) []schemaPair {
	t.Helper()
	docs := map[string][]byte{"config": schemaJSON, "keywords": []byte(keywordSchema)}
	for name, typ := range registry.New("test") {
		docs["channel "+name] = typ.SettingsSchema
	}
	var out []schemaPair
	for _, name := range sortedKeys(docs) {
		ref, err := refCompile(docs[name])
		if err != nil {
			t.Fatalf("%s: the reference does not compile it: %v", name, err)
		}
		own, err := compileNode(docs[name])
		if err != nil {
			t.Fatalf("%s: the validator does not compile it: %v", name, err)
		}
		out = append(out, schemaPair{name, ref, own})
	}
	return out
}

// agree checks that both validators give the same answer for value: the same violations, in the
// same order, or the same refusal of a value JSON cannot express.
func agree(t testing.TB, p schemaPair, label string, value any) {
	t.Helper()
	want, werr := refViolations(p.ref, value)
	got, gerr := nodeViolations(p.own, value)
	if (werr == nil) != (gerr == nil) || fmt.Sprint(want) != fmt.Sprint(got) {
		t.Errorf("%s, %s: reference %v (err %v); validator %v (err %v)", p.name, label, want, werr, got, gerr)
	}
}

// agreeAll runs every schema over value and over every mapping inside it, so the channel schemas
// see the channels entries and the config schema sees each nested part.
func agreeAll(t testing.TB, pairs []schemaPair, label string, value any) {
	t.Helper()
	for _, sub := range mappings(value) {
		for _, p := range pairs {
			agree(t, p, label, sub)
		}
	}
}

// mappings is v, if it is a mapping, and every mapping inside it.
func mappings(v any) []any {
	var out []any
	switch x := v.(type) {
	case map[string]any:
		out = append(out, x)
		for _, k := range sortedKeys(x) {
			out = append(out, mappings(x[k])...)
		}
	case []any:
		for _, e := range x {
			out = append(out, mappings(e)...)
		}
	}
	return out
}

// craftedDocs make every keyword in the schemas fail at least once, alone and together.
var craftedDocs = []string{
	``,
	`version: 1`,
	`version: 2`,
	`version: "1"`,
	`version: 1.0`,
	`version: 1.5`,
	`version: 1e0`,
	`version: true`,
	`version: null`,
	`version: [1]`,
	`nope: 1`,
	`{a: 1, b: 2, version: 1}`,
	`"bad\tkey": 1`,
	`human: {name: Sam}`,
	`human: {name: 7}`,
	`human: {nick: x}`,
	`human: x`,
	`human: null`,
	`deadline: 1s`,
	`deadline: 5`,
	`incident_ttl: [24h]`,
	`channels: {}`,
	`channels: []`,
	`channels: {pager: {type: pagerduty, routing_key: {secret: pd}}}`,
	`channels: {Pager: {type: log}}`,
	`channels: {-pager: {type: log}}`,
	`channels: {"": {type: log}}`,
	`channels: {a-very-long-channel-name-that-goes-on-and-on-and-on-past-sixty-three-chars: {type: log}}`,
	`channels: {pager: {}}`,
	`channels: {pager: {type: ""}}`,
	`channels: {pager: {type: 5}}`,
	`channels: {pager: x}`,
	`channels: {pager: null}`,
	`channels: {Bad: x, good: {}}`,
	`channels: {"ü": {type: log}}`,
	`channels: {"1": {type: log}}`,
	`channels: {1: {type: log}}`,
	`channels: {journal: {type: log, path: /tmp/x.log}}`,
	`channels: {journal: {type: log, path: ""}}`,
	`channels: {journal: {type: log, path: 5, extra: 1}}`,
	`channels: {phone: {type: ntfy, server: "https://ntfy.example", topic: {secret: t}}}`,
	`channels: {phone: {type: ntfy, server: "http://ntfy.example", topic: {secret: t}, token: {secret: k, x: 1}}}`,
	`channels: {phone: {type: ntfy, server: "https:///", topic: TOPIC}}`,
	`channels: {phone: {type: ntfy, topic: {}, token: {secret: 5}}}`,
	`channels: {phone: {type: bark, server: "https://bark.example", device_key: {secret: d}}}`,
	`channels: {phone: {type: bark, device_key: [d]}}`,
	`channels: {pager: {type: pagerduty, events_url: "ftp://x", routing_key: {secret: r}}}`,
	`channels: {pager: {type: pagerduty, events_url: 5}}`,
	`{server: "https://x", topic: {secret: t}, token: {secret: k}}`,
	`{server: "https://x", device_key: {secret: d}}`,
	`{events_url: "https://x", routing_key: {secret: r}}`,
	`{path: /tmp/x}`,
	`{path: ""}`,
	`{path: "ü"}`,
	`{secret: 5}`,
	`levels: []`,
	`levels: {}`,
	`levels: [x]`,
	`levels: [{}]`,
	`levels: [{name: page}]`,
	`levels: [{name: Page}]`,
	`levels: [{name: 5}]`,
	`levels: [{name: page, extra: 1}]`,
	`levels: [{name: page, use_when: 5, ask: maybe, quiet_hours: never}]`,
	`levels: [{name: page, ask: required, quiet_hours: hold}, {name: note, ask: optional, quiet_hours: send}]`,
	`levels: [{name: page, retry: {every: 1m, times: 3}}]`,
	`levels: [{name: page, retry: {every: 1m, times: -1}}]`,
	`levels: [{name: page, retry: {every: 5, times: 1.5}}]`,
	`levels: [{name: page, retry: {times: 2.0}}]`,
	`levels: [{name: page, retry: {times: "3"}}]`,
	`levels: [{name: page, retry: {often: true}}]`,
	`levels: [{name: page, retry: x}]`,
	`levels: [{name: page, reminder: 1h}]`,
	`levels: [{name: page, reminder: 60}]`,
	`levels: [{name: page, send: [{channel: pager, interruption: critical, options: {a: 1}}]}]`,
	`levels: [{name: page, send: [{channel: pager, interruption: loud}]}]`,
	`levels: [{name: page, send: [{channel: "", interruption: active}]}]`,
	`levels: [{name: page, send: [{}]}]`,
	`levels: [{name: page, send: [{channel: pager, interruption: active, options: [1]}]}]`,
	`levels: [{name: page, send: [{channel: pager, interruption: active, x: 1}]}]`,
	`levels: [{name: page, send: x}]`,
	`levels: [{name: page, send: [x, null, 5]}]`,
	`quiet_hours: {timezone: Europe/London, windows: [{days: [mon, sun], from: "22:00", to: "07:30"}]}`,
	`quiet_hours: {timezone: "", windows: []}`,
	`quiet_hours: {windows: []}`,
	`quiet_hours: {}`,
	`quiet_hours: {timezone: UTC, windows: [{}]}`,
	`quiet_hours: {timezone: UTC, windows: [{days: [funday], from: "24:00", to: "7:30"}]}`,
	`quiet_hours: {timezone: UTC, windows: [{days: mon, from: 2200, to: "23:60"}]}`,
	`quiet_hours: {timezone: UTC, windows: [{days: [], from: "00:00", to: "23:59", x: 1}]}`,
	`quiet_hours: {timezone: UTC, windows: x, extra: 1}`,
	`quiet_hours: {timezone: 5, windows: [{days: [mon], from: "22:00\n", to: "07:00"}]}`,
	`shim: {default: {level: page, ask: x}, low: {level: note}, agent: claude}`,
	`shim: {default: {level: 5}, low: x, agent: 5}`,
	`shim: {default: {mode: x}, extra: 1}`,
	`shim: []`,
	`version: 2
nope: 1
human: x
channels: {Bad: 1}
levels: [{}]
quiet_hours: {}
shim: {low: 5}`,
	`{version: 1, channels: {a: {type: log, path: /tmp/a.log}}, levels: [{name: note, use_when: always, send: [{channel: a, interruption: passive}]}]}`,
}

// TestValidatorAgreesWithJSONSchemaOnCraftedDocs holds the validator to the reference on inputs
// that make every keyword fail.
func TestValidatorAgreesWithJSONSchemaOnCraftedDocs(t *testing.T) {
	pairs := schemaPairs(t)
	for i, doc := range craftedDocs {
		var v any
		if err := yaml.Unmarshal([]byte(doc), &v); err != nil {
			t.Fatalf("crafted doc %d does not parse: %v", i, err)
		}
		agreeAll(t, pairs, fmt.Sprintf("crafted doc %d %q", i, doc), v)
	}
}

// TestValidatorAgreesWithJSONSchemaOnEveryFixture holds the validator to the reference on every
// config in the repository: the files under any testdata directory, and every string literal in
// the Go sources that parses as a YAML or JSON mapping (the test configs and the starter config).
func TestValidatorAgreesWithJSONSchemaOnEveryFixture(t *testing.T) {
	pairs := schemaPairs(t)
	n := 0
	for label, doc := range repoFixtures(t) {
		var v any
		if yaml.Unmarshal([]byte(doc), &v) != nil {
			continue
		}
		if _, ok := v.(map[string]any); !ok {
			continue
		}
		n++
		agreeAll(t, pairs, label, v)
	}
	if n < 100 {
		t.Errorf("only %d fixtures parsed as a mapping; the harvest is broken", n)
	}
}

// repoFixtures is every candidate config in the module: testdata files that are YAML or JSON, and
// string literals that mention a top-level config key.
func repoFixtures(t *testing.T) map[string]string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return harvest(root, path, d, out)
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// harvest adds the fixtures in one file of the walk, and skips hidden, fuzz and dist directories.
func harvest(root, path string, d fs.DirEntry, out map[string]string) error {
	rel, _ := filepath.Rel(root, path)
	switch {
	case d.IsDir() && skipDir(rel, d.Name()):
		return filepath.SkipDir
	case d.IsDir():
		return nil
	case strings.Contains(rel, "testdata") && isConfigFile(path):
		b, err := os.ReadFile(path) //nolint:gosec // a file of this module
		if err != nil {
			return err
		}
		out[rel] = string(b)
	case strings.HasSuffix(path, ".go"):
		return literals(path, rel, out)
	}
	return nil
}

// skipDir is a hidden directory (the module root aside), a fuzz corpus or the build output.
func skipDir(rel, name string) bool {
	return strings.HasPrefix(name, ".") && rel != "." || name == "fuzz" || name == "dist"
}

func isConfigFile(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".yaml" || ext == ".yml" || ext == ".json"
}

var configKeys = []string{"version:", "channels:", "levels:", "quiet_hours:", "shim:", "human:", `"version"`, `"channels"`}

// literals adds every string literal in a Go file that mentions a config key.
func literals(path, rel string, out map[string]string) error {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		return err
	}
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		for _, k := range configKeys {
			if strings.Contains(s, k) {
				out[fmt.Sprintf("%s offset %d", rel, lit.Pos())] = s
				break
			}
		}
		return true
	})
	return nil
}

// FuzzValidatorAgreesWithJSONSchema holds the validator to the reference on any YAML or JSON. The
// seeds are the crafted docs; testdata/fuzz holds the corpus a local fuzzing run found.
func FuzzValidatorAgreesWithJSONSchema(f *testing.F) {
	for _, doc := range craftedDocs {
		f.Add([]byte(doc))
	}
	pairs := schemaPairs(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		var v any
		if yaml.Unmarshal(data, &v) != nil {
			return
		}
		agreeAll(t, pairs, fmt.Sprintf("%q", data), v)
	})
}
