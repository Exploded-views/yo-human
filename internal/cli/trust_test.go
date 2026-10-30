package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The userinfo URL below is split in the source so the public-repo scrub stays uniform.
const projectBody = `version: 1
deadline: 7s
channels:
  office:
    type: ntfy
    server: "https://ntfy.example/alerts?token=tk_FAKE"
    topic: { secret: office-topic }
  oncall:
    type: pagerduty
    events_url: "https://user:pw` + "@" + `events.project.example/v2/enqueue"
    routing_key: { secret: office-pd }
  sloppy:
    type: pagerduty
    routing_key: FAKE-LITERAL-KEY
  odd:
    type: ntfy
    server: "https://%zz"
    topic: { secret: Pasted-FAKE-TOKEN }
  bare:
    type: ntfy
    server: "ntfy.example/x"
  journal:
    type: log
    path: ~/.zshrc
  loud:
    type: bark
    server: "https://api.day.app"
    device_key: { secret: bark-key }
    sound: alarm
    volume: 5
  mystery:
    type: carrier-pigeon
    token: FAKE-UNKNOWN-TYPE-VALUE
levels:
  - { name: page, use_when: x, send: [ { channel: journal, interruption: critical }, { channel: office, interruption: critical } ] }
  - { name: quiet, use_when: y }
`

func TestConfigTrustThenChange(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	cwd, _ := os.Getwd()
	proj := write(t, filepath.Join(cwd, ".yo-human.yaml"), "version: 1\ndeadline: 7s\n")
	real, _ := filepath.EvalSymlinks(proj)

	// Untrusted: ignored, with the notice and the command to run on stderr.
	_, out, errb := s.run("", "config", "check")
	mustContain(t, "stderr", errb, "ignoring the project config", "not trusted", "yo-human config trust "+real)
	mustNotContain(t, "stdout", out, real)

	code, out, errb := s.run("", "config", "trust", ".yo-human.yaml")
	if code != ExitOK || firstLine(out) != "RESULT ok" || errb != "" {
		t.Fatalf("trust: code=%d out=%q err=%q", code, out, errb)
	}
	_, out, errb = s.run("", "config", "check")
	mustContain(t, "origins after trust", out, "deadline <- "+real)
	mustNotContain(t, "stderr after trust", errb, "ignoring")

	// An edit un-trusts it again.
	write(t, proj, "version: 1\ndeadline: 9s\n")
	_, out, errb = s.run("", "config", "check")
	mustContain(t, "stderr after the edit", errb, "ignoring the project config", "has changed since it was")
	mustNotContain(t, "origins after the edit", out, "deadline <- "+real)
}

// The trust key is the symlink-resolved path, the form config.Load looks up.
func TestConfigTrustThroughASymlinkedCwd(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	realDir := filepath.Join(s.home, "real")
	write(t, filepath.Join(realDir, ".yo-human.yaml"), "version: 1\ndeadline: 7s\n")
	link := filepath.Join(s.home, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}
	t.Chdir(link)
	if code, _, errb := s.run("", "config", "trust", ".yo-human.yaml"); code != ExitOK {
		t.Fatalf("trust: code=%d err=%q", code, errb)
	}
	resolved, _ := filepath.EvalSymlinks(filepath.Join(realDir, ".yo-human.yaml"))
	_, out, errb := s.run("", "config", "check")
	mustContain(t, "origins", out, "deadline <- "+resolved)
	mustNotContain(t, "stderr", errb, "ignoring")

	// Trusting by the symlink's own path records the same key.
	if err := os.Symlink(filepath.Join(realDir, ".yo-human.yaml"), filepath.Join(s.home, "alias.yaml")); err != nil {
		t.Fatal(err)
	}
	code, out, _ := s.run("", "config", "trust", filepath.Join(s.home, "alias.yaml"), "--json")
	mustContain(t, "trust through a file symlink", out, `"path":"`+resolved+`"`)
	if code != ExitOK {
		t.Errorf("code=%d", code)
	}
}

func TestConfigTrustShowsWhatTheFileWouldDo(t *testing.T) {
	s := newSandbox(t)
	proj := write(t, filepath.Join(s.home, "proj", ".yo-human.yaml"), projectBody)
	r := s.do("", "config", "trust", proj)
	expect(t, r.code == ExitOK && r.err == "", "code=%d err=%q", r.code, r.err)
	mustContain(t, "trust output", r.out,
		`channel "office" has type "ntfy"`,
		`channel "office" server "https://ntfy.example/alerts" (credentials, query or fragment hidden)`,
		`channel "office" topic uses secret "office-topic"`,
		`channel "oncall" events_url "https://events.project.example/v2/enqueue" (credentials, query or fragment hidden)`,
		`channel "oncall" routing_key uses secret "office-pd"`,
		`channel "odd" server <not a valid URL>`,
		`channel "bare" server <not a valid URL>`,
		`channel "sloppy" routing_key holds a literal value (not shown; config check refuses it)`,
		`channel "odd" topic holds a literal value (not shown; config check refuses it)`,
		`channel "loud" sound "alarm"`,
		`channel "loud" volume 5`,
		`channel "loud" device_key uses secret "bark-key"`,
		`channel "mystery" has type "carrier-pigeon"`,
		`channel "mystery" settings are not shown (unknown channel type)`,
		`level "page" sends to "journal" (critical), "office" (critical)`,
		`level "quiet" sends nowhere`)
	mustNotContain(t, "trust output", r.out+r.err, "tk_FAKE", "user:pw", "FAKE-LITERAL-KEY", "Pasted-FAKE-TOKEN", "FAKE-UNKNOWN-TYPE-VALUE")
}

// A trusted project file can point a log channel at a shell startup file; the display must say so.
func TestConfigTrustShowsALogPathAimedAtAStartupFile(t *testing.T) {
	s := newSandbox(t)
	proj := write(t, filepath.Join(s.home, "proj", ".yo-human.yaml"), projectBody)
	r := s.do("", "config", "trust", proj)
	mustContain(t, "trust output", r.out, `channel "journal" has type "log"`, `channel "journal" path '~/.zshrc'`)
}

func TestConfigTrustJSONHoldsTheSameDisplay(t *testing.T) {
	s := newSandbox(t)
	proj := write(t, filepath.Join(s.home, "proj", ".yo-human.yaml"), projectBody)
	r := s.do("", "config", "trust", proj, "--json")
	var obj struct {
		Status   string
		Channels []struct {
			Name, Type string
			Settings   map[string]string
		}
		Levels []struct {
			Name  string
			Sends []struct{ Channel, Interruption string }
		}
	}
	err := json.Unmarshal([]byte(r.out), &obj)
	expect(t, err == nil && r.code == ExitOK && r.err == "" && obj.Status == "ok", "code=%d out=%q err=%q (%v)", r.code, r.out, r.err, err)
	byName := map[string]map[string]string{}
	for _, c := range obj.Channels {
		byName[c.Name] = c.Settings
	}
	expect(t, byName["journal"]["path"] == "'~/.zshrc'" && byName["office"]["topic"] == `uses secret "office-topic"`, "channels = %+v", obj.Channels)
	expect(t, len(obj.Levels) == 2 && obj.Levels[0].Name == "page" && len(obj.Levels[0].Sends) == 2 && obj.Levels[0].Sends[1].Channel == "office" && obj.Levels[0].Sends[1].Interruption == "critical", "levels = %+v", obj.Levels)
	mustNotContain(t, "json", r.out, "FAKE-LITERAL-KEY", "tk_FAKE", "user:pw")
}

// With no file given, trust acts on the nearest project file, the one Load would read.
func TestConfigTrustWithNoFileTrustsTheNearestProjectFile(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	cwd, _ := os.Getwd()
	proj := write(t, filepath.Join(cwd, ".yo-human.yaml"), "version: 1\ndeadline: 7s\n")
	sub := filepath.Join(cwd, "deep", "er")
	expect(t, os.MkdirAll(sub, 0o700) == nil, "mkdir")
	t.Chdir(sub)
	r := s.do("", "config", "trust")
	r.succeeds(t, "trust")
	real, _ := filepath.EvalSymlinks(proj)
	mustContain(t, "trust output", r.out, "trusting "+strconv.Quote(real))
	_, out, _ := s.run("", "config", "check")
	mustContain(t, "origins", out, "deadline <- "+real)

	t.Chdir(t.TempDir())
	s.do("", "config", "trust").fails(t, "no project file", ExitUsage, "no project config file")
}

func TestConfigTrustRefusals(t *testing.T) {
	s := newSandbox(t)
	big := write(t, filepath.Join(s.home, "big.yaml"), "x: \""+strings.Repeat("a", 1<<20)+"\"\n")
	notMap := write(t, filepath.Join(s.home, "list.yaml"), "- a\n- b\n")
	broken := write(t, filepath.Join(s.home, "broken.yaml"), "a: [unclosed\n")
	locked := write(t, filepath.Join(s.home, "locked.yaml"), "version: 1\n")
	expect(t, os.Chmod(locked, 0) == nil, "chmod")
	for _, c := range []struct {
		name string
		args []string
		want int
		msg  string
	}{
		{"two arguments", []string{"a", "b"}, ExitUsage, "error: config trust takes at most one file"},
		{"missing file", []string{filepath.Join(s.home, "nope.yaml")}, ExitUsage, "error: there is no file at"},
		{"a directory", []string{s.home}, ExitUsage, "is not a regular file"},
		{"over 1 MiB", []string{big}, ExitConfig, "larger than 1 MiB"},
		{"a list", []string{notMap}, ExitConfig, "is not a YAML or JSON mapping"},
		{"broken yaml", []string{broken}, ExitConfig, "is not a YAML or JSON mapping"},
		{"unreadable", []string{locked}, ExitUsage, "error: cannot open"},
	} {
		s.do("", append([]string{"config", "trust"}, c.args...)...).fails(t, c.name, c.want, c.msg)
	}
	st := filepath.Join(s.home, ".local", "state", "yo-human")
	_, err := os.Stat(st)
	expect(t, err != nil, "a refused trust still opened the store at %s", st)
}

func TestConfigTrustStoreFailures(t *testing.T) {
	s := newSandbox(t)
	good := write(t, filepath.Join(s.home, "good.yaml"), "version: 1\n")
	// The trust record cannot be written: trust.json is a directory.
	expect(t, os.MkdirAll(filepath.Join(s.home, ".local", "state", "yo-human", "trust.json"), 0o700) == nil, "mkdir")
	s.do("", "config", "trust", good).fails(t, "trust record unwritable", ExitStore, "error: ")
	// The state directory cannot be created, for trust or for any command that loads the config.
	blocker := write(t, filepath.Join(s.home, "state-blocker"), "file")
	t.Setenv("XDG_STATE_HOME", blocker)
	s.do("", "config", "trust", good).fails(t, "state dir blocked", ExitStore, "error: ")
	s.do("", "levels").fails(t, "levels with the state dir blocked", ExitStore, "error: ")
}

// Without a working directory a relative path cannot be resolved.
func TestConfigTrustWithoutAWorkingDirectory(t *testing.T) {
	s := newSandbox(t)
	old := absPath
	absPath = func(string) (string, error) { return "", errors.New("getwd: no such file or directory") }
	t.Cleanup(func() { absPath = old })
	if code, _, errb := s.run("", "config", "trust", ".yo-human.yaml"); code != ExitUsage || !strings.Contains(errb, "cannot find the working directory") {
		t.Errorf("code=%d err=%q; want exit 64", code, errb)
	}
}

// A file that breaks the schema would fail every command once trusted (Load refuses it), so trust
// refuses it first: before the prompt, and with nothing recorded.
func TestConfigTrustRefusesAFileThatBreaksTheSchema(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	shown := s.tty("yes\n")
	proj := write(t, filepath.Join(s.home, "proj", ".yo-human.yaml"), "version: 1\ncolour: red\n")
	r := s.do("", "config", "trust", proj)
	r.fails(t, "trust of a file that breaks the schema", ExitConfig, "breaks the config schema")
	expect(t, shown.String() == "", "the prompt was shown: %q", shown.String())
	_, err := os.Stat(filepath.Join(s.home, ".local", "state", "yo-human", "trust.json"))
	expect(t, errors.Is(err, os.ErrNotExist), "a trust record was written: %v", err)
}
