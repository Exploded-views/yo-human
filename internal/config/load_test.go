package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/problem"
)

func fixturePaths(t *testing.T) config.Paths {
	t.Helper()
	return config.Paths{
		Explicit: abs(t, "testdata/explicit.yaml"),
		Cwd:      abs(t, "testdata/project"),
		UserDir:  abs(t, "testdata/user"),
		System:   abs(t, "testdata/system.yaml"),
	}
}

func trustedProject(t *testing.T) truster {
	t.Helper()
	p := canon(t, "testdata/project/.yo-human.yaml")
	e := canon(t, "testdata/explicit.yaml") // outside the user directory, so it needs trust too
	return truster{p: sha(t, p), e: sha(t, e)}
}

func TestLayerOrderClosestWins(t *testing.T) {
	c, notices, err := config.Load(fixturePaths(t), registry(), trustedProject(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(notices) != 0 {
		t.Errorf("notices = %v, want none", notices)
	}
	if c.Deadline != 2*time.Second || c.IncidentTTL != time.Hour || c.Human.Name != "Pat" {
		t.Errorf("Deadline %v, IncidentTTL %v, Human %q; want 2s (project), 1h (explicit), Pat (user)",
			c.Deadline, c.IncidentTTL, c.Human.Name)
	}
	wantMergedChannels(t, c)
	if len(c.Levels) != 1 || c.Levels[0].Name != "project-only" {
		t.Errorf("Levels = %+v, want the project ladder alone", c.Levels)
	}
	for k, want := range map[string]string{
		"deadline": canon(t, "testdata/project/.yo-human.yaml"), "incident_ttl": "testdata/explicit.yaml",
		"human": "testdata/user/config.yaml", "levels": canon(t, "testdata/project/.yo-human.yaml"),
		"channels.phone": "testdata/user/config.yaml", "channels.desk": "testdata/system.yaml",
	} {
		if got := c.Origin[k]; got != abs(t, want) {
			t.Errorf("Origin[%q] = %q, want %q", k, got, abs(t, want))
		}
	}
}

func wantMergedChannels(t *testing.T, c *config.Config) {
	t.Helper()
	for _, want := range []string{"desk", "phone", "journal", "repo"} {
		if _, ok := c.Channels[want]; !ok {
			t.Errorf("channels lack %q (channels merge by name): %v", want, c.Channels)
		}
	}
	tok := c.Channels["phone"].Settings["token"]
	if !reflect.DeepEqual(tok, map[string]any{"secret": "user-token"}) {
		t.Errorf("phone token = %v, want the user file's instance to replace the system one whole", tok)
	}
}

func TestUntrustedProjectIgnoredWithNotice(t *testing.T) {
	proj := canon(t, "testdata/project/.yo-human.yaml")
	e := canon(t, "testdata/explicit.yaml")
	for name, tr := range map[string]truster{
		"never trusted": {e: sha(t, e)},
		"changed":       {proj: strings.Repeat("0", 64), e: sha(t, e)},
	} {
		t.Run(name, func(t *testing.T) {
			c, notices, err := config.Load(fixturePaths(t), registry(), tr)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if len(notices) != 1 || !strings.Contains(notices[0].Text, "yo-human config trust") ||
				!strings.Contains(notices[0].Text, proj) {
				t.Errorf("notices = %+v, want one naming the file and `yo-human config trust`", notices)
			}
			if c.Deadline != 3*time.Second {
				t.Errorf("Deadline = %v, want 3s: the project file must be ignored", c.Deadline)
			}
			if _, ok := c.Channels["repo"]; ok || len(c.Levels) != 2 {
				t.Errorf("an untrusted project file leaked: channels %v, %d levels", c.Channels, len(c.Levels))
			}
		})
	}
	noExplicit := fixturePaths(t)
	noExplicit.Explicit = ""
	if _, _, err := config.Load(noExplicit, registry(), nil); err != nil {
		t.Errorf("a nil truster must treat the project file as untrusted, got %v", err)
	}
}

func TestProjectNearestAncestor(t *testing.T) {
	root := t.TempDir()
	root = canon(t, root)
	far := write(t, root, ".yo-human.yaml", "deadline: 9s\n")
	near := write(t, filepath.Join(root, "a"), ".yo-human.json", `{"deadline": "4s"}`)
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	p := config.Paths{Cwd: deep, UserDir: filepath.Join(root, "none")}
	c, _, err := config.Load(p, registry(), truster{near: sha(t, near), far: sha(t, far)})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Deadline != 4*time.Second || c.Origin["deadline"] != near {
		t.Errorf("Deadline = %v from %q, want 4s from %q (the nearest ancestor)", c.Deadline, c.Origin["deadline"], near)
	}
}

func TestDefaults(t *testing.T) {
	c := mustLoadExplicit(t, minimal)
	if c.Deadline != time.Second || c.IncidentTTL != 24*time.Hour || c.Version != 1 {
		t.Errorf("Deadline %v, IncidentTTL %v, Version %d; want 1s, 24h, 1", c.Deadline, c.IncidentTTL, c.Version)
	}
	if len(c.Levels) != 1 {
		t.Fatalf("Levels = %+v, want one", c.Levels)
	}
	l := c.Levels[0]
	if !l.AskRequired || l.HoldInQuietHours || l.Retry != (config.Retry{Every: 60 * time.Second, Times: 5}) || l.Reminder != 0 {
		t.Errorf("level defaults = %+v, want ask required, send in quiet hours, retry 60s x5", l)
	}
	if c.Shim != nil {
		t.Errorf("Shim = %+v, want nil without a shim block", c.Shim)
	}
}

func TestLevelFieldsDecoded(t *testing.T) {
	c := mustLoadExplicit(t, `
channels:
  n: { type: pager }
levels:
  - name: a
    use_when: x
    ask: optional
    quiet_hours: hold
    retry: { every: 30s }
    reminder: 30m
    send:
      - { channel: n, interruption: time-sensitive, options: { sound: x } }
quiet_hours:
  timezone: America/New_York
  windows:
    - { days: [mon, fri], from: "22:00", to: "07:30" }
shim:
  default: { level: a, ask: look now }
  low: { level: a, ask: "" }
  agent: orchestrator
`)
	if len(c.Levels) != 1 || len(c.Levels[0].Send) != 1 {
		t.Fatalf("Levels = %+v, want one level with one send", c.Levels)
	}
	wantLevel(t, c.Levels[0])
	wantQuietHours(t, c.QuietHours)
	want := &config.Shim{Default: config.ShimMode{Level: "a", Ask: "look now"}, Low: config.ShimMode{Level: "a"}, Agent: "orchestrator"}
	if !reflect.DeepEqual(c.Shim, want) {
		t.Errorf("Shim = %+v, want %+v", c.Shim, want)
	}
}

func wantLevel(t *testing.T, l config.Level) {
	t.Helper()
	if l.AskRequired || !l.HoldInQuietHours || l.Reminder != 30*time.Minute ||
		l.Retry != (config.Retry{Every: 30 * time.Second, Times: 5}) {
		t.Errorf("level = %+v", l)
	}
	if s := l.Send[0]; s.Channel != "n" || s.Interruption != channel.TimeSensitive || s.Options["sound"] != "x" {
		t.Errorf("send = %+v", s)
	}
}

func wantQuietHours(t *testing.T, q config.QuietHours) {
	t.Helper()
	if q.Location == nil || q.Location.String() != "America/New_York" || len(q.Windows) != 1 {
		t.Fatalf("quiet hours = %+v", q)
	}
	w := q.Windows[0]
	if !reflect.DeepEqual(w.Days, []time.Weekday{time.Monday, time.Friday}) || int(w.From) != 22*60 || int(w.To) != 7*60+30 {
		t.Errorf("window = %+v", w)
	}
}

func TestJSONConfigAccepted(t *testing.T) {
	y := mustLoadExplicit(t, minimal+"deadline: 2s\n")
	dir := t.TempDir()
	jsonDoc := `{"version":1,"deadline":"2s","channels":{"n":{"type":"pager"}},
	  "levels":[{"name":"only","use_when":"testing","send":[{"channel":"n","interruption":"passive"}]}]}`
	p := config.Paths{Explicit: write(t, dir, "c.json", jsonDoc), Cwd: dir, UserDir: dir}
	j, _, err := config.Load(p, registry(), truster{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	y.Origin, j.Origin = nil, nil
	if !reflect.DeepEqual(y, j) {
		t.Errorf("json config differs:\nyaml %+v\njson %+v", y, j)
	}
}

func TestRank(t *testing.T) {
	c, _, err := config.Load(config.Paths{Explicit: abs(t, "testdata/user/config.yaml"), Cwd: t.TempDir(), UserDir: abs(t, "testdata/user")}, registry(), truster{})
	if err != nil {
		t.Fatal(err)
	}
	if i, ok := c.Rank("low"); !ok || i != 0 {
		t.Errorf("Rank(low) = %d, %v", i, ok)
	}
	if i, ok := c.Rank("high"); !ok || i != 1 {
		t.Errorf("Rank(high) = %d, %v", i, ok)
	}
	if _, ok := c.Rank("nope"); ok {
		t.Error("Rank of an unknown level must report false")
	}
}

func TestLoadProblems(t *testing.T) {
	cases := []struct{ name, content, what string }{
		{"unknown top-level key", minimal + "colour: red\n", "colour"},
		{"wrong type", "deadline: [1]\n", "deadline"},
		{"bad level shape", "levels: [{name: a, send: [{channel: n, interruption: loud}]}]\n", "levels.0.send.0.interruption"},
		{"bad duration", minimal + "deadline: soon\n", "deadline"},
		{"bad reminder", strings.Replace(minimal, "use_when: testing", "use_when: t\n    reminder: tomorrow", 1), "reminder"},
		{"bad timezone", minimal + "quiet_hours: {timezone: Mars/Base, windows: []}\n", "timezone"},
		{"bad clock", minimal + "quiet_hours: {timezone: UTC, windows: [{days: [mon], from: '25:00', to: '07:00'}]}\n", "windows"},
		{"not yaml", "a: [unclosed\n", "not valid"},
		{"not a mapping", "- a\n- b\n", "mapping"},
		{"bad version", "version: 2\n", "version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := loadExplicit(t, tc.content)
			p := wantProblem(t, err, problem.Config)
			if !strings.Contains(p.What, tc.what) {
				t.Errorf("What = %q, want it to contain %q", p.What, tc.what)
			}
			if !strings.Contains(p.What, "c.yaml") {
				t.Errorf("What = %q, want it to name the file", p.What)
			}
			if p.Valid == "" || p.Fix == "" {
				t.Errorf("Valid %q and Fix %q must both be set", p.Valid, p.Fix)
			}
		})
	}
}

func TestLoadMissingFiles(t *testing.T) {
	dir := t.TempDir()
	_, _, err := config.Load(config.Paths{Explicit: filepath.Join(dir, "gone.yaml"), Cwd: dir}, registry(), truster{})
	if p := wantProblem(t, err, problem.Config); !strings.Contains(p.What, "gone.yaml") {
		t.Errorf("What = %q, want the explicit file named", p.What)
	}
	_, _, err = config.Load(config.Paths{Cwd: dir, UserDir: filepath.Join(dir, "none"), System: filepath.Join(dir, "none.yaml")}, registry(), truster{})
	if p := wantProblem(t, err, problem.Config); !strings.Contains(p.Fix, "yo-human config init") {
		t.Errorf("Fix = %q, want it to name `yo-human config init`", p.Fix)
	}
}

func TestDefaultPaths(t *testing.T) {
	t.Setenv("YO_HUMAN_CONFIG", "/x/explicit.yaml")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	t.Setenv("HOME", "/home/user")
	got := config.DefaultPaths("/work")
	want := config.Paths{Explicit: "/x/explicit.yaml", Cwd: "/work", UserDir: "/xdg/yo-human", System: "/etc/yo-human/config.yaml", Home: "/home/user"}
	if got != want {
		t.Errorf("DefaultPaths = %+v, want %+v", got, want)
	}
	t.Setenv("YO_HUMAN_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/user")
	if got := config.DefaultPaths("/work"); got.UserDir != "/home/user/.config/yo-human" || got.Explicit != "" {
		t.Errorf("fallback paths = %+v", got)
	}
}
