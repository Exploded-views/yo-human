package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/config"
)

func TestConfigInitWritesStarterAndRefusesOverwrite(t *testing.T) {
	s := newSandbox(t)
	r := s.do("", "config", "init")
	r.succeeds(t, "init")
	expect(t, r.err == "", "init stderr = %q", r.err)
	mustContain(t, "init output", r.out, s.userConfig())
	fi, err := os.Stat(s.userConfig())
	expect(t, err == nil && fi.Mode().Perm() == 0o600, "starter file: %v, %v; want mode 0600", fi, err)
	body, _ := os.ReadFile(s.userConfig())

	r = s.do("", "config", "init")
	r.fails(t, "second init", ExitUsage, "already exists")
	mustContain(t, "second init", r.err, "error: the config file ")
	again, _ := os.ReadFile(s.userConfig())
	expect(t, string(again) == string(body), "the second init changed the file")
}

// The starter is meant to be filled in: it passes check, and stderr lists the secrets to set.
func TestStarterPassesCheckAndHoldsNoSecretValues(t *testing.T) {
	s := newSandbox(t)
	s.do("", "config", "init").succeeds(t, "init")
	r := s.do("", "config", "check")
	r.succeeds(t, "check of the starter")
	mustContain(t, "check stderr", r.err, "ntfy-topic", "pd-critical", "yo-human secret set ntfy-topic", "yo-human secret set pd-critical")
	mustNotContain(t, "check stderr", r.err, "error:")

	body, _ := os.ReadFile(s.userConfig())
	var doc struct{ Channels map[string]map[string]any }
	expect(t, yamlUnmarshal(body, &doc) == nil, "the starter is not YAML")
	for name, field := range map[string]string{"phone": "topic", "pager": "routing_key"} {
		ref, _ := doc.Channels[name][field].(map[string]any)
		_, ok := ref["secret"]
		expect(t, ok, "channels.%s.%s = %v, want a { secret: name } reference", name, field, doc.Channels[name][field])
	}
}

// The starter must not point at a real public service: `channel test` on it would publish there.
func TestStarterPointsAtAPlaceholderServer(t *testing.T) {
	s := newSandbox(t)
	s.do("", "config", "init").succeeds(t, "init")
	body, _ := os.ReadFile(s.userConfig())
	mustContain(t, "starter", string(body), "server: https://ntfy.example.com\n")
	mustNotContain(t, "starter", string(body), "https://ntfy.sh")
}

func TestConfigInitHonoursConfigFlag(t *testing.T) {
	s := newSandbox(t)
	target := filepath.Join(s.home, "elsewhere", "mine.yaml")
	expect(t, os.MkdirAll(filepath.Dir(target), 0o700) == nil, "mkdir") // init creates no directory for an explicit path
	s.do("", "config", "init", "--config", target).succeeds(t, "init --config")
	_, err := os.Stat(target)
	expect(t, err == nil, "--config target not written: %v", err)
	_, err = os.Stat(s.userConfig())
	expect(t, err != nil, "init --config also wrote the user config")
	s.do("", "config", "init", "extra").fails(t, "extra argument", ExitUsage, "config init takes no arguments")
}

func TestConfigInitStoreFailures(t *testing.T) {
	s := newSandbox(t)
	blocker := write(t, filepath.Join(s.home, "blocker"), "x")
	s.do("", "config", "init", "--config", filepath.Join(blocker, "sub", "c.yaml")).fails(t, "parent is a file", ExitUsage, "does not exist")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(blocker, "sub"))
	s.do("", "config", "init").fails(t, "user config directory under a file", ExitStore, "error: cannot create the directory")
	t.Setenv("XDG_CONFIG_HOME", "")
	dir := filepath.Join(s.home, "readonly")
	expect(t, os.Mkdir(dir, 0o500) == nil, "mkdir")
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	s.do("", "config", "init", "--config", filepath.Join(dir, "new.yaml")).fails(t, "read-only directory", ExitStore, "error: ")
}

func TestConfigCheckReportsOrigins(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	explicit := write(t, s.ownConfig("override.yaml"), "version: 1\ndeadline: 5s\n")
	code, out, errb := s.run("", "config", "check", "--config", explicit)
	if code != ExitOK || firstLine(out) != "RESULT ok" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errb)
	}
	mustContain(t, "origins", out, "deadline <- "+explicit, "channels.phone <- "+s.userConfig(), "levels <- "+s.userConfig(), "3 channels, 2 levels")

	code, out, _ = s.run("", "config", "check", "--json", "--config", explicit)
	var obj struct {
		Version int
		Status  string
		Origins map[string]string
	}
	if err := json.Unmarshal([]byte(out), &obj); err != nil || code != ExitOK || obj.Status != "ok" || obj.Origins["deadline"] != explicit {
		t.Errorf("json: code=%d out=%q (%v)", code, out, err)
	}
}

func TestConfigCheckMissingSecretsAreWarningsNotErrors(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	code, out, errb := s.run("", "config", "check")
	if code != ExitOK || firstLine(out) != "RESULT ok" {
		t.Fatalf("code=%d out=%q err=%q; want exit 0", code, out, errb)
	}
	mustContain(t, "stderr", errb,
		`warning: secret "ntfy-topic", used by channels.phone.topic, is not set`,
		`fix: tell the human to run, in their own terminal: op read "op://<vault>/<item>/<field>" | yo-human secret set ntfy-topic`,
		`warning: secret "pd-critical", used by channels.pager.routing_key, is not set`,
		`yo-human secret set pd-critical`)
	mustNotContain(t, "stderr", errb, "error:")

	if code, _, errb = s.run("FAKE-TOPIC\n", "secret", "set", "ntfy-topic"); code != ExitOK {
		t.Fatalf("secret set: %d %q", code, errb)
	}
	_, _, errb = s.run("", "config", "check")
	mustNotContain(t, "stderr after setting ntfy-topic", errb, "ntfy-topic", "FAKE-TOPIC")
	mustContain(t, "stderr after setting ntfy-topic", errb, "pd-critical")
}

func TestConfigCheckAppliesEachChannelsOwnValidation(t *testing.T) {
	s := newSandbox(t)
	// events_url passes the JSON schema (https) but the pagerduty type refuses a query string.
	write(t, s.userConfig(), strings.Replace(userConfigBody, `routing_key: { secret: pd-critical }`,
		`routing_key: { secret: pd-critical }, events_url: "https://events.example.com/v2?x=1"`, 1)+`  - name: extra
    use_when: "x"
    send:
      - { channel: pager, interruption: critical, options: { urgency: high } }
`)
	code, out, errb := s.run("", "config", "check")
	if code != ExitConfig || out != "" {
		t.Fatalf("code=%d out=%q err=%q; want exit 78 and no RESULT", code, out, errb)
	}
	mustContain(t, "stderr", errb, `error: channel "pager" is misconfigured`, "events_url", "fix: edit channels.pager")
	mustNotContain(t, "stderr", errb, "x=1") // the value is not echoed

	// With a good events_url the per-send options still fail the type's own validation.
	write(t, s.userConfig(), userConfigBody+`  - name: extra
    use_when: "x"
    send:
      - { channel: pager, interruption: critical, options: { urgency: high } }
`)
	code, _, errb = s.run("", "config", "check")
	mustContain(t, "stderr", errb, `error: level "extra" sends to channel "pager" with options it refuses`)
	if code != ExitConfig {
		t.Errorf("code=%d, want 78", code)
	}
}

func TestConfigCheckWithoutAConfigExits78(t *testing.T) {
	r := newSandbox(t).do("", "config", "check")
	r.fails(t, "no config", ExitConfig, "config init")
	mustContain(t, "stderr", r.err, "error: no config file was found")
}

func TestConfigCheckStructureErrorsExit78(t *testing.T) {
	s := newSandbox(t)
	write(t, s.userConfig(), strings.Replace(userConfigBody, "type: log", "type: smoke-signal", 1))
	r := s.do("", "config", "check")
	lines := strings.Split(strings.TrimSpace(r.err), "\n")
	r.fails(t, "unknown type", ExitConfig, "error: channels.journal has unknown type")
	expect(t, len(lines) == 3 && strings.HasPrefix(lines[1], "valid: ") && strings.HasPrefix(lines[2], "fix: "), "stderr = %q; want three lines", r.err)
}

func TestConfigCheckLooseSecretsFileExits78(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	write(t, s.secretsFile(), "pd-critical: FAKE-ROUTING\n")
	expect(t, os.Chmod(s.secretsFile(), 0o644) == nil, "chmod")
	r := s.do("", "config", "check")
	r.fails(t, "loose secrets file", ExitConfig, "chmod 600")
	mustNotContain(t, "stderr", r.err, "FAKE-ROUTING")
	s.do("", "config", "check", "extra").fails(t, "extra argument", ExitUsage, "config check takes no arguments")
}

func TestConfigCheckPrintsCapabilityWarningsAsWarnings(t *testing.T) {
	s := newSandbox(t)
	// ntfy has no critical alerts: advice, not a fault.
	write(t, s.userConfig(), strings.Replace(userConfigBody, "{ channel: phone, interruption: time-sensitive }", "{ channel: phone, interruption: critical }", 1))
	code, out, errb := s.run("", "config", "check")
	if code != ExitOK || firstLine(out) != "RESULT ok" {
		t.Fatalf("code=%d out=%q err=%q; want exit 0", code, out, errb)
	}
	mustContain(t, "stderr", errb, "warning: level \"page\" sends critical to channel \"phone\"")
	mustNotContain(t, "stderr", errb, "error:")
}

func TestConfigSchemaPrintsTheEmbeddedSchema(t *testing.T) {
	s := newSandbox(t)
	code, out, _ := s.run("", "config", "schema")
	if code != ExitOK || firstLine(out) != "RESULT ok" || !strings.Contains(out, string(config.Schema())) {
		t.Errorf("text: code=%d out=%.80q", code, out)
	}
	code, out, _ = s.run("", "config", "schema", "--json")
	var obj struct {
		Status string
		Schema struct {
			Title string `json:"title"`
		}
	}
	if err := json.Unmarshal([]byte(out), &obj); err != nil || code != ExitOK || obj.Status != "ok" || obj.Schema.Title != "yo-human config" {
		t.Errorf("json: code=%d out=%.80q (%v)", code, out, err)
	}
	if code, _, _ := s.run("", "config", "schema", "extra"); code != ExitUsage {
		t.Errorf("extra argument: code=%d", code)
	}
}

func TestConfigCheckJSONCarriesWarningsAndLeavesStderrEmpty(t *testing.T) {
	s := newSandbox(t)
	// ntfy cannot do critical (a capability warning) and no secret is stored (a missing-secret warning).
	write(t, s.userConfig(), strings.Replace(userConfigBody, "{ channel: phone, interruption: time-sensitive }", "{ channel: phone, interruption: critical }", 1))
	r := s.do("", "config", "check", "--json")
	var obj struct {
		Status   string
		Warnings []struct{ What, Valid, Fix string }
	}
	err := json.Unmarshal([]byte(r.out), &obj)
	expect(t, err == nil && r.code == ExitOK && r.err == "" && obj.Status == "ok", "code=%d out=%q err=%q (%v)", r.code, r.out, r.err, err)
	var whats []string
	for _, w := range obj.Warnings {
		expect(t, w.Valid != "" && w.Fix != "", "warning %+v lacks valid or fix", w)
		whats = append(whats, w.What)
	}
	mustContain(t, "warnings", strings.Join(whats, "\n"), `secret "ntfy-topic"`, `secret "pd-critical"`, `sends critical to channel "phone"`)
}

func TestConfigCheckJSONWarningsIsAnEmptyArrayWhenClean(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	s.do("FAKE-ONE\n", "secret", "set", "ntfy-topic").succeeds(t, "set")
	s.do("FAKE-TWO\n", "secret", "set", "pd-critical").succeeds(t, "set")
	r := s.do("", "config", "check", "--json")
	mustContain(t, "check json", r.out, `"warnings":[]`)
}

func TestConfigCheckReportsEveryStructureErrorNotJustTheFirst(t *testing.T) {
	s := newSandbox(t)
	write(t, s.userConfig(), strings.Replace(strings.Replace(userConfigBody, "type: log", "type: smoke-signal", 1), "type: pagerduty", "type: carrier-pigeon", 1))
	r := s.do("", "config", "check")
	r.fails(t, "two unknown types", ExitConfig, "error: channels.journal has unknown type")
	mustContain(t, "stderr", r.err, "error: channels.pager has unknown type")
}

func TestConfigCheckRefusesALogChannelFromAProjectFile(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	proj := write(t, ".yo-human.yaml", "version: 1\nchannels:\n  journal: { type: log, path: /Users/user/.zshrc }\n")
	s.do("", "config", "trust", proj).succeeds(t, "trust")
	r := s.do("", "config", "check")
	r.fails(t, "project log channel", ExitConfig, "channels.journal")
	mustContain(t, "stderr", r.err, "a project file may not define one", "define log channels in your user config")
}

func TestConfigInitNeverReplacesAnythingAtThePath(t *testing.T) {
	s := newSandbox(t)
	dangling := filepath.Join(s.configDir(), "config.yaml")
	expect(t, os.MkdirAll(s.configDir(), 0o700) == nil, "mkdir")
	expect(t, os.Symlink(filepath.Join(s.home, "nowhere"), dangling) == nil, "symlink")
	s.do("", "config", "init").fails(t, "init over a dangling symlink", ExitUsage, "already exists")
	_, err := os.Stat(filepath.Join(s.home, "nowhere"))
	expect(t, err != nil, "init wrote through the symlink")
}

// config check prints the merged result, each setting with the file it came from. A secret field
// shows its {secret: name} reference, never a value.
func TestConfigCheckPrintsTheMergedConfig(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	s.setSecret("pd-critical")
	r := s.do("", "config", "check")
	expect(t, r.code == ExitOK, "config check: %+v", r)
	mustContain(t, "stdout", r.out,
		"channels.pager <- "+s.userConfig()+` = {"routing_key":{"secret":"pd-critical"},"type":"pagerduty"}`,
		"levels <- "+s.userConfig()+` = [{"ask":"optional","name":"note"`)
	mustNotContain(t, "stdout", r.out, "FAKE-PD-CRITICAL")
	r = s.do("", "config", "check", "--json")
	var obj struct {
		Merged  map[string]any
		Origins map[string]string
	}
	expect(t, json.Unmarshal([]byte(r.out), &obj) == nil && obj.Merged["channels"] != nil && obj.Origins["levels"] == s.userConfig(),
		"config check --json: %s; want the merged config and its origins", r.out)
}
