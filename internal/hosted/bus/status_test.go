package bus_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

func validStatus(swarm string) json.RawMessage {
	return json.RawMessage(`{"swarm":"` + swarm + `","sessions":[{"name":"build-1","state":"working"},{"name":"review-2","state":"idle"}],` +
		`"waiting_on_human":["ob-12"],"main_health":"ok","spend":12.5,"ts":1793000000}`)
}

func TestStatusPushAndSwarmStatus(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.b.PushStatus(ctx, cinode, validStatus("crew")); err != nil {
		t.Fatal(err)
	}
	if err := e.b.PushStatus(ctx, node("ci-9", "client"), validStatus("client")); err != nil {
		t.Fatal(err)
	}
	all, err := e.b.SwarmStatus(ctx, duet, "")
	if err != nil || len(all) != 2 || all[0].Swarm != "client" || all[1].Swarm != "crew" {
		t.Fatalf("all = %+v, %v", all, err)
	}
}

func TestStatusDocRoundTrips(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.b.PushStatus(ctx, cinode, validStatus("crew")); err != nil {
		t.Fatal(err)
	}
	one, err := e.b.SwarmStatus(ctx, duet, "crew")
	if err != nil || len(one) != 1 || one[0].Updated.IsZero() {
		t.Fatalf("one = %+v, %v", one, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(one[0].Doc, &doc); err != nil || doc["swarm"] != "crew" || doc["main_health"] != "ok" {
		t.Errorf("doc = %s, %v", one[0].Doc, err)
	}
}

func TestStatusPushReplacesAndAppendsAnEvent(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, health := range []string{"ok", "degraded"} {
		doc := json.RawMessage(strings.Replace(string(validStatus("crew")), "ok", health, 1))
		if err := e.b.PushStatus(ctx, cinode, doc); err != nil {
			t.Fatal(err)
		}
	}
	one, _ := e.b.SwarmStatus(ctx, duet, "crew")
	if len(one) != 1 || !strings.Contains(string(one[0].Doc), "degraded") {
		t.Errorf("a second push did not replace the first: %+v", one)
	}
	if got := types(e.events(t)); len(got) != 2 || got[0] != bus.EventStatusPushed {
		t.Errorf("events = %v, want a status_pushed for each push", got)
	}
}

func TestStatusReadsAreDuetOnly(t *testing.T) {
	e := newEnv(t)
	for _, p := range []bus.Principal{cinode, {Kind: bus.PrincipalDesk}, {Kind: bus.PrincipalPasskey}, {}} {
		_, err := e.b.SwarmStatus(context.Background(), p, "")
		wantCode(t, err, bus.CodeForbidden)
	}
	_, err := e.b.SwarmStatus(context.Background(), duet, "Not A Swarm")
	wantCode(t, err, bus.CodeInvalid)
}

func TestStatusPushOnlyForNodes(t *testing.T) {
	e := newEnv(t)
	for _, p := range []bus.Principal{duet, {Kind: bus.PrincipalDesk}, {Kind: bus.PrincipalNode, Name: "x", Swarm: "duet"}, {}} {
		wantCode(t, e.b.PushStatus(context.Background(), p, validStatus("crew")), bus.CodeForbidden)
	}
}

func TestStatusPushRejectsOtherSwarm(t *testing.T) {
	e := newEnv(t)
	err := e.b.PushStatus(context.Background(), cinode, validStatus("client"))
	if be := errWithCode(t, err, bus.CodeInvalid); be.What != "swarm" {
		t.Errorf("What = %q, want swarm", be.What)
	}
	if st, _ := e.b.SwarmStatus(context.Background(), duet, ""); len(st) != 0 {
		t.Errorf("stored %+v", st)
	}
}

func TestStatusPushRejectsBadShape(t *testing.T) {
	good := string(validStatus("crew"))
	cases := map[string]struct{ doc, field string }{
		"not json":          {`{`, "status"},
		"an array":          {`[]`, "status"},
		"null":              {`null`, "status"},
		"trailing data":     {good + `{}`, "status"},
		"too big":           {`{"swarm":"crew","pad":"` + strings.Repeat("x", 16<<10) + `"}`, "status"},
		"unknown field":     {strings.Replace(good, `"spend"`, `"extra":1,"spend"`, 1), "status"},
		"missing sessions":  {`{"swarm":"crew","waiting_on_human":[],"main_health":"ok","spend":0,"ts":1}`, "status"},
		"missing ts":        {`{"swarm":"crew","sessions":[],"waiting_on_human":[],"main_health":"ok","spend":0}`, "status"},
		"bad state":         {strings.Replace(good, `"working"`, `"asleep"`, 1), "sessions.state"},
		"empty name":        {strings.Replace(good, `"build-1"`, `""`, 1), "sessions.name"},
		"sessions not list": {strings.Replace(good, `"sessions":[{"name":"build-1","state":"working"},{"name":"review-2","state":"idle"}]`, `"sessions":{}`, 1), "status"},
		"negative spend":    {strings.Replace(good, `12.5`, `-1`, 1), "spend"},
		"string spend":      {strings.Replace(good, `12.5`, `"12.5"`, 1), "status"},
		"fractional ts":     {strings.Replace(good, `1793000000`, `1.5`, 1), "status"},
		"negative ts":       {strings.Replace(good, `1793000000`, `-1`, 1), "ts"},
		"bad health":        {strings.Replace(good, `"ok"`, `"all fine, really"`, 1), "main_health"},
		"control in name":   {strings.Replace(good, `build-1`, `build\u0007`, 1), "sessions.name"},
		"long entry":        {strings.Replace(good, `"ob-12"`, `"`+strings.Repeat("x", 81)+`"`, 1), "waiting_on_human"},
		"too many sessions": {`{"swarm":"crew","sessions":[` + strings.Repeat(`{"name":"a","state":"idle"},`, 64) + `{"name":"a","state":"idle"}],"waiting_on_human":[],"main_health":"ok","spend":0,"ts":1}`, "sessions"},
	}
	for name, c := range cases {
		e := newEnv(t)
		err := e.b.PushStatus(context.Background(), cinode, json.RawMessage(c.doc))
		if be := errWithCode(t, err, bus.CodeInvalid); be.What != c.field {
			t.Errorf("%s: What = %q, want %q", name, be.What, c.field)
		}
		if st, _ := e.b.SwarmStatus(context.Background(), duet, ""); len(st) != 0 || len(e.events(t)) != 0 {
			t.Errorf("%s: something was stored", name)
		}
	}
}

func TestStatusIsStoredCanonical(t *testing.T) {
	// What is read back is what was validated, re-encoded from the parsed fields: no whitespace
	// or key order of the sender's survives.
	e := newEnv(t)
	doc := ` { "ts" : 5, "spend":0, "main_health":"ok", "waiting_on_human":[], "sessions":[], "swarm":"crew" } `
	if err := e.b.PushStatus(context.Background(), cinode, json.RawMessage(doc)); err != nil {
		t.Fatal(err)
	}
	st, _ := e.b.SwarmStatus(context.Background(), duet, "crew")
	want := `{"swarm":"crew","sessions":[],"waiting_on_human":[],"main_health":"ok","spend":0,"ts":5}`
	if string(st[0].Doc) != want {
		t.Errorf("doc = %s, want %s", st[0].Doc, want)
	}
}

func TestWalledStatusDocIdsOnly(t *testing.T) {
	e := newEnvWalled(t, "client")
	n := node("ci-9", "client")
	ok := `{"swarm":"client","sessions":[{"name":"s1","state":"working"}],"waiting_on_human":["ob-12"],"main_health":"ok","spend":3,"ts":9}`
	if err := e.b.PushStatus(context.Background(), n, json.RawMessage(ok)); err != nil {
		t.Fatalf("a typed document: %v", err)
	}
	bad := map[string]struct{ doc, field string }{
		"free-text session name": {strings.Replace(ok, `s1`, `Fix the Acme payroll bug`, 1), "sessions.name"},
		"upper-case name":        {strings.Replace(ok, `s1`, `Build`, 1), "sessions.name"},
		"41-char name":           {strings.Replace(ok, `s1`, strings.Repeat("a", 41), 1), "sessions.name"},
		"free-text waiting":      {strings.Replace(ok, `ob-12`, `needs the owner for the Acme deploy`, 1), "waiting_on_human"},
		"zero id":                {strings.Replace(ok, `ob-12`, `ob-0`, 1), "waiting_on_human"},
		"leading-zero id":        {strings.Replace(ok, `ob-12`, `ob-012`, 1), "waiting_on_human"},
		"seven-digit id":         {strings.Replace(ok, `ob-12`, `ob-1234567`, 1), "waiting_on_human"},
		"a handle that is prose": {strings.Replace(ok, `ob-12`, `ob-12 now`, 1), "waiting_on_human"},
	}
	for name, c := range bad {
		err := e.b.PushStatus(context.Background(), n, json.RawMessage(c.doc))
		if be := errWithCode(t, err, bus.CodeInvalid); be.What != c.field {
			t.Errorf("%s: What = %q, want %q", name, be.What, c.field)
		}
	}
	// The same prose is fine for an unwalled swarm: the rule is the wall's, not the grammar's.
	free := strings.Replace(strings.Replace(ok, `build-1`, `Fix the build`, 1), `"swarm":"client"`, `"swarm":"crew"`, 1)
	if err := e.b.PushStatus(context.Background(), cinode, json.RawMessage(free)); err != nil {
		t.Errorf("unwalled swarm: %v", err)
	}
}
