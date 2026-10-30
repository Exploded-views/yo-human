package config_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// valid is a config Check accepts: one pager instance with a secret reference, one log-only
// instance, and a ladder of two levels.
func valid() *config.Config {
	return &config.Config{
		Version: 1,
		Channels: map[string]config.ChannelInstance{
			"phone":   {Type: "pager", Settings: map[string]any{"token": map[string]any{"secret": "phone-token"}}},
			"journal": {Type: "logonly", Settings: map[string]any{}},
			"soft":    {Type: "quiet", Settings: map[string]any{}},
		},
		Levels: []config.Level{
			{Name: "low", UseWhen: "fyi", Send: []config.Send{{Channel: "phone", Interruption: channel.Passive}, {Channel: "journal", Interruption: channel.Passive}}},
			{Name: "high", UseWhen: "now", Send: []config.Send{{Channel: "phone", Interruption: channel.Critical}}},
		},
		Shim: &config.Shim{Default: config.ShimMode{Level: "high"}, Low: config.ShimMode{Level: "low"}},
	}
}

func TestCheckAcceptsValidConfig(t *testing.T) {
	if got := config.Check(valid(), registry()); len(got) != 0 {
		t.Errorf("Check = %+v, want no problems (a missing secret is not a Check finding)", got)
	}
}

func TestCheckFindings(t *testing.T) {
	cases := []struct {
		name        string
		mutate      func(*config.Config)
		what, fix   string
		wantWarning bool
	}{
		{"level without use_when", func(c *config.Config) { c.Levels[0].UseWhen = "" },
			`level "low" has no use_when`, "use_when", false},
		{"send to an unknown instance", func(c *config.Config) { c.Levels[1].Send[0].Channel = "ghost" },
			`unknown channel "ghost"`, "channels", false},
		{"unknown channel type", func(c *config.Config) { c.Channels["phone"] = config.ChannelInstance{Type: "teleport"} },
			`channels.phone has unknown type "teleport"`, "pager", false},
		{"interruption the type cannot do", func(c *config.Config) {
			c.Levels[1].Send[0] = config.Send{Channel: "soft", Interruption: channel.Critical}
		},
			"critical", "pager", true},
		{"no channel reaches a human", func(c *config.Config) { c.Levels[0].Send = c.Levels[0].Send[1:] },
			`level "low" has no channel that reaches a human`, "add", false},
		{"level with no sends", func(c *config.Config) { c.Levels[0].Send = nil },
			`level "low" has no channel that reaches a human`, "add", false},
		{"duplicate level names", func(c *config.Config) { c.Levels[1].Name, c.Shim.Default.Level = "low", "low" },
			`duplicate level name "low"`, "rename", false},
		{"shim names an unknown level", func(c *config.Config) { c.Shim.Low.Level = "ghost" },
			`shim.low.level names unknown level "ghost"`, "levels", false},
		{"settings violate the type's schema", func(c *config.Config) { c.Channels["phone"].Settings["topic"] = 5 },
			"channels.phone.topic", "pager", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := valid()
			tc.mutate(c)
			got := config.Check(c, registry())
			if len(got) != 1 {
				t.Fatalf("Check = %+v, want exactly one problem", got)
			}
			p := got[0]
			if p.Kind != problem.Config || !strings.Contains(p.What, tc.what) || !strings.Contains(p.Fix, tc.fix) || p.Valid == "" {
				t.Errorf("problem = %+v, want What containing %q and Fix containing %q", p, tc.what, tc.fix)
			}
			if config.IsWarning(p) != tc.wantWarning {
				t.Errorf("IsWarning = %v, want %v", config.IsWarning(p), tc.wantWarning)
			}
		})
	}
}

func TestCheckWarningNamesTheTypesThatCan(t *testing.T) {
	c := valid()
	c.Levels[1].Send[0] = config.Send{Channel: "soft", Interruption: channel.Critical}
	got := config.Check(c, registry())
	if len(got) != 1 {
		t.Fatalf("Check = %+v, want exactly one problem", got)
	}
	p := got[0]
	if !strings.Contains(p.Fix, "pager") || !strings.Contains(p.Fix, "logonly") || strings.Contains(p.Fix, "quiet") {
		t.Errorf("Fix = %q, want it to name pager and logonly, not quiet", p.Fix)
	}
}

func TestCheckIsDeterministic(t *testing.T) {
	c := valid()
	for _, n := range []string{"a", "b", "c", "d"} {
		c.Channels[n] = config.ChannelInstance{Type: "teleport"}
	}
	first := config.Check(c, registry())
	if len(first) < 4 {
		t.Fatalf("Check = %+v, want a problem for each bad channel", first)
	}
	for i := 0; i < 20; i++ {
		got := config.Check(c, registry())
		for j := range first {
			if got[j].What != first[j].What {
				t.Fatalf("order differs: %q vs %q", got[j].What, first[j].What)
			}
		}
	}
}

const fakeLiteral = "R0UTING-FAKE-VALUE"

func noValue(t *testing.T, p *problem.Problem) {
	t.Helper()
	for _, s := range []string{p.What, p.Valid, p.Fix, p.Error()} {
		if strings.Contains(s, fakeLiteral) {
			t.Errorf("problem text holds the secret value: %q", s)
		}
	}
}

func TestLiteralInSecretFieldIsRefusedWithoutEcho(t *testing.T) {
	c := valid()
	c.Channels["phone"] = config.ChannelInstance{Type: "pager", Settings: map[string]any{"token": fakeLiteral}}
	got := config.Check(c, registry())
	if len(got) != 1 {
		t.Fatalf("Check = %+v, want exactly one problem", got)
	}
	if !strings.Contains(got[0].What, "channels.phone.token") || got[0].Kind != problem.Config {
		t.Errorf("problem = %+v, want it to name channels.phone.token", got[0])
	}
	noValue(t, got[0])
	if !strings.Contains(got[0].Fix, "yo-human secret set") {
		t.Errorf("Fix = %q, want the secret set command", got[0].Fix)
	}

	_, err := c.Instances(map[string]string{}, registry())
	p := wantProblem(t, err, problem.Config)
	noValue(t, p)
	if !strings.Contains(p.What, "channels.phone.token") {
		t.Errorf("Instances What = %q, want channels.phone.token", p.What)
	}
	// every shape of non-reference is refused the same way
	for _, bad := range []any{map[string]any{"secret": 5}, map[string]any{"secret": "a", "x": fakeLiteral}, []any{fakeLiteral}, 12} {
		c.Channels["phone"].Settings["token"] = bad
		_, err := c.Instances(map[string]string{"a": "V"}, registry())
		p := wantProblem(t, err, problem.Config)
		noValue(t, p)
		if !strings.Contains(p.What, "must be a secret reference") {
			t.Errorf("What = %q, want the reference rule, not a missing-secret message", p.What)
		}
		for _, p := range config.Check(c, registry()) {
			noValue(t, p)
		}
	}
}

func TestSchemaViolationsNeverEchoValues(t *testing.T) {
	c := valid()
	c.Channels["phone"].Settings["topic"] = []any{fakeLiteral}
	c.Channels["phone"].Settings["extra-"+"key"] = fakeLiteral
	got := config.Check(c, registry())
	if len(got) == 0 {
		t.Fatal("want schema findings")
	}
	for _, p := range got {
		noValue(t, p)
	}
}

func TestInstancesResolveSecrets(t *testing.T) {
	c := valid()
	c.Channels["phone"].Settings["topic"] = "ops"
	got, err := c.Instances(map[string]string{"phone-token": "TOK-FAKE-1", "unused": "OTHER-FAKE-VALUE"}, registry())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	ph := got["phone"]
	if ph.Name != "phone" || ph.Type != "pager" || ph.Secrets["token"] != "TOK-FAKE-1" || ph.Settings["topic"] != "ops" {
		t.Errorf("phone = %+v", ph)
	}
	if _, ok := ph.Settings["token"]; ok {
		t.Errorf("Settings still hold the token field: %+v", ph.Settings)
	}
	if len(got) != 3 || len(got["journal"].Secrets) != 0 {
		t.Errorf("instances = %+v, want 3, the log-only one without secrets", got)
	}
	if _, ok := c.Channels["phone"].Settings["token"]; !ok {
		t.Error("Instances must not mutate the Config")
	}
}

func TestInstancesMissingSecret(t *testing.T) {
	_, err := valid().Instances(map[string]string{}, registry())
	p := wantProblem(t, err, problem.Config)
	if !strings.Contains(p.What, "phone-token") {
		t.Errorf("What = %q, want the secret name", p.What)
	}
	if want := `tell the human to run, in their own terminal: op read "op://<vault>/<item>/<field>" | yo-human secret set phone-token`; p.Fix != want {
		t.Errorf("Fix = %q, want %q", p.Fix, want)
	}
}

func TestInstancesUnknownType(t *testing.T) {
	c := valid()
	c.Channels["phone"] = config.ChannelInstance{Type: "teleport"}
	_, err := c.Instances(map[string]string{}, registry())
	if p := wantProblem(t, err, problem.Config); !strings.Contains(p.What, "teleport") {
		t.Errorf("What = %q, want the type named", p.What)
	}
}

func TestSchemaIsValidJSONAndRejectsUnknownKeys(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(config.Schema(), &doc); err != nil {
		t.Fatalf("Schema is not JSON: %v", err)
	}
	if doc["additionalProperties"] != false {
		t.Errorf("additionalProperties = %v, want false at the top level", doc["additionalProperties"])
	}
}
