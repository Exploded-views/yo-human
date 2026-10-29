package bus_test

import (
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

func TestVisibilityByKind(t *testing.T) {
	b := bus.New(nil, nil, bus.Limits{})
	crew, client := bus.Item{Swarm: "crew"}, bus.Item{Swarm: "client"}
	for _, tc := range []struct {
		name         string
		p            bus.Principal
		crew, client bool
	}{
		{"passkey", bus.Principal{Kind: bus.PrincipalPasskey, Name: "phone"}, true, true},
		{"duet", bus.Principal{Kind: bus.PrincipalDuet}, true, true},
		{"desk", bus.Principal{Kind: bus.PrincipalDesk}, true, true},
		{"crew node", node("ci-1", "crew"), true, false},
		{"client node", node("ci-9", "client"), false, true},
		{"node without swarm", bus.Principal{Kind: bus.PrincipalNode, Name: "x"}, false, false},
		{"desk_start", bus.Principal{Kind: bus.PrincipalDeskStart}, false, false},
		{"executor", bus.Principal{Kind: bus.PrincipalExecutor}, false, false},
		{"zero", bus.Principal{}, false, false},
	} {
		if got := b.Visible(tc.p, crew); got != tc.crew {
			t.Errorf("%s sees crew item = %v, want %v", tc.name, got, tc.crew)
		}
		if got := b.Visible(tc.p, client); got != tc.client {
			t.Errorf("%s sees client item = %v, want %v", tc.name, got, tc.client)
		}
	}
	if b.Visible(bus.Principal{}, bus.Item{}) {
		t.Error("a zero principal sees an item with no swarm")
	}
}
