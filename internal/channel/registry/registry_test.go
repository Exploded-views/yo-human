package registry_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel/registry"
)

func TestNewHoldsTheFourTypes(t *testing.T) {
	reg := registry.New("test")
	var names []string
	for k, v := range reg {
		names = append(names, k)
		if v.Name != k || v.New == nil || len(v.SettingsSchema) == 0 {
			t.Errorf("type %q: name %q, New %v, schema %d bytes", k, v.Name, v.New != nil, len(v.SettingsSchema))
		}
	}
	sort.Strings(names)
	if strings.Join(names, " ") != "bark log ntfy pagerduty" {
		t.Errorf("registry = %v, want bark log ntfy pagerduty", names)
	}
}

func TestNewBuildsAFreshMapEachCall(t *testing.T) {
	a, b := registry.New("test"), registry.New("test")
	delete(a, "ntfy")
	if _, ok := b["ntfy"]; !ok {
		t.Error("deleting from one registry changed another")
	}
}
