package cli

import (
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/problem"
)

func TestRegistryHoldsTheFourTypes(t *testing.T) {
	reg := channelTypes()
	var names []string
	for k, v := range reg {
		names = append(names, k)
		if v.Name != k || v.New == nil {
			t.Errorf("type %q: name %q, New %v", k, v.Name, v.New != nil)
		}
	}
	sort.Strings(names)
	if strings.Join(names, " ") != "bark log ntfy pagerduty" {
		t.Errorf("registry = %v, want bark log ntfy pagerduty", names)
	}
}

func cfgWith(chans map[string]config.ChannelInstance) *config.Config {
	return &config.Config{Channels: chans}
}

func ref(name string) map[string]any { return map[string]any{"secret": name} }

func lazyConfig() *config.Config {
	return cfgWith(map[string]config.ChannelInstance{
		"journal": {Type: "log", Settings: map[string]any{"path": "/tmp/x.log"}},
		"pager":   {Type: "pagerduty", Settings: map[string]any{"routing_key": ref("pd-unset")}},
	})
}

// Building only the named channels is what lets notify survive an unset secret on a channel its
// level does not use.
func TestBuildChannelsBuildsOnlyTheNamedInstances(t *testing.T) {
	got, err := buildChannels(lazyConfig(), channelTypes(), map[string]string{}, []string{"journal"})
	expect(t, err == nil && len(got) == 1 && got["journal"] != nil, "got %v, %v; want only journal and no error (pager's secret is unset but unused)", got, err)
	got, err = buildChannels(lazyConfig(), channelTypes(), nil, nil)
	expect(t, err == nil && len(got) == 0, "no names: got %v, %v; want nothing built", got, err)
}

func TestBuildChannelsNeedsTheNamedChannelsSecrets(t *testing.T) {
	_, err := buildChannels(lazyConfig(), channelTypes(), map[string]string{}, []string{"journal", "pager"})
	p, ok := problem.As(err)
	expect(t, ok, "err = %v; want a Problem", err)
	expect(t, p.Kind == problem.Config && strings.Contains(p.What, "pd-unset"), "problem = %+v", p)
	expect(t, strings.Contains(p.Fix, "yo-human secret set pd-unset"), "fix = %q", p.Fix)
	got, err := buildChannels(lazyConfig(), channelTypes(), map[string]string{"pd-unset": "FAKE-ROUTING"}, []string{"pager"})
	expect(t, err == nil && len(got) == 1 && got["pager"].Type() == "pagerduty", "with the secret set: got %v, %v; want pager built", got, err)
}

func TestBuildChannelsRejectsAnUnknownName(t *testing.T) {
	_, err := buildChannels(cfgWith(nil), channelTypes(), nil, []string{"ghost"})
	if p, ok := problem.As(err); !ok || p.Kind != problem.Config || !strings.Contains(p.What, `"ghost"`) {
		t.Errorf("err = %v; want a Config problem naming ghost", err)
	}
}

func TestBuildChannelsReportsEveryRefusalNamingTheInstance(t *testing.T) {
	cfg := cfgWith(map[string]config.ChannelInstance{
		"a-pager": {Type: "pagerduty", Settings: map[string]any{"routing_key": ref("k"), "events_url": "http://insecure.example.com"}},
		"b-pager": {Type: "pagerduty", Settings: map[string]any{"routing_key": ref("k"), "events_url": "https://x.example.com/p?q=1"}},
		"c-log":   {Type: "log", Settings: map[string]any{"path": "/tmp/x.log"}},
	})
	built, err := buildChannels(cfg, channelTypes(), map[string]string{"k": "FAKE-ROUTING"}, []string{"a-pager", "b-pager", "c-log"})
	var joined interface{ Unwrap() []error }
	if err == nil || !errors.As(err, &joined) || len(joined.Unwrap()) != 2 {
		t.Fatalf("err = %v; want both refusals joined", err)
	}
	mustContain(t, "error", err.Error(), `"a-pager"`, `"b-pager"`)
	if len(built) != 1 || built["c-log"] == nil {
		t.Errorf("built %v; want the channel that built beside the error", built)
	}
	for _, e := range joined.Unwrap() {
		if p, ok := problem.As(e); !ok || p.Kind != problem.Config {
			t.Errorf("%v is not a Config problem", e)
		}
	}
}

// The hooks that let a test inject fakes and move the clock are nil unless a test sets them, so the
// binary never uses them.
func TestTestHooksAreUnsetByDefault(t *testing.T) {
	newSandbox(t)
	if registryHook != nil || clockHook != nil {
		t.Fatal("a test hook is set outside the test that installed it")
	}
	if d := time.Since(now()); d < 0 || d > time.Minute {
		t.Errorf("now() is %v behind the real clock; want the real clock", d)
	}
	if ntfyNew := channelTypes()["ntfy"].New; ntfyNew == nil {
		t.Error("the ntfy type has no constructor")
	}
}
