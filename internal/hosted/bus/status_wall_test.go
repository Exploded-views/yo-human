package bus_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

func statusDocWith(swarm, health, spend, ts string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"swarm":%q,"sessions":[],"waiting_on_human":[],"main_health":%s,"spend":%s,"ts":%s}`, swarm, health, spend, ts))
}

func pushTo(e *env, swarm string, doc json.RawMessage) error {
	return e.b.PushStatus(context.Background(), node("ci-9", swarm), doc)
}

// main_health is one of four values in every swarm: a free token would be a channel across the wall.
func TestWalledStatusMainHealthIsAnEnum(t *testing.T) {
	e := newEnvWalled(t, "client")
	for _, swarm := range []string{"client", "crew"} {
		for _, h := range []string{"ok", "degraded", "failing", "unknown"} {
			if err := pushTo(e, swarm, statusDocWith(swarm, `"`+h+`"`, "0", "1")); err != nil {
				t.Errorf("%s %s: %v", swarm, h, err)
			}
		}
		for _, h := range []string{"payroll-outage-at-bigco", "green", "OK", "ok-", ""} {
			err := pushTo(e, swarm, statusDocWith(swarm, `"`+h+`"`, "0", "1"))
			if be := errWithCode(t, err, bus.CodeInvalid); be.What != "main_health" {
				t.Errorf("%s %q: What = %q", swarm, h, be.What)
			}
		}
	}
}

func TestWalledStatusDigitsAreBounded(t *testing.T) {
	e := newEnvWalled(t, "client")
	for _, c := range []struct{ spend, ts string }{
		{"0", "0"}, {"9999999", "8589934592"}, {"1234567.89", "1793000000"}, {"0.5", "1"}, {"12", "1"},
	} {
		if err := pushTo(e, "client", statusDocWith("client", `"ok"`, c.spend, c.ts)); err != nil {
			t.Errorf("spend %s ts %s: %v", c.spend, c.ts, err)
		}
	}
	for _, c := range []struct{ spend, ts, field string }{
		{"0", "9223372036854775807", "ts"},
		{"0", "8589934593", "ts"},
		{"1234567.891234567", "1", "spend"},
		{"12345678", "1", "spend"},
		{"1.234", "1", "spend"},
		{"1e3", "1", "spend"},
		{"1E2", "1", "spend"},
		{"-1", "1", "spend"},
	} {
		err := pushTo(e, "client", statusDocWith("client", `"ok"`, c.spend, c.ts))
		if be := errWithCode(t, err, bus.CodeInvalid); be.What != c.field {
			t.Errorf("spend %s ts %s: What = %q, want %s", c.spend, c.ts, be.What, c.field)
		}
	}
	// The bound is the wall's: an unwalled swarm keeps the plain number rules.
	if err := pushTo(e, "crew", statusDocWith("crew", `"ok"`, "1234567.891234567", "9223372036854775807")); err != nil {
		t.Errorf("unwalled: %v", err)
	}
}

// Text that is not valid UTF-8 is refused, not repaired: the decoder would swap U+FFFD in.
func TestStatusPushRefusesInvalidUTF8(t *testing.T) {
	e := newEnv(t)
	named := func(name string) json.RawMessage {
		return json.RawMessage(`{"swarm":"crew","sessions":[{"name":"` + name + `","state":"idle"}],"waiting_on_human":[],"main_health":"ok","spend":0,"ts":1}`)
	}
	for name, c := range map[string]struct {
		doc   json.RawMessage
		field string
	}{
		"raw bytes":              {named("a\xff\xfeb"), "status"},
		"escaped lone surrogate": {named(`a\ud800b`), "sessions.name"},
		"the replacement itself": {named("a\ufffdb"), "sessions.name"},
	} {
		err := pushTo(e, "crew", c.doc)
		if be := errWithCode(t, err, bus.CodeInvalid); be.What != c.field {
			t.Errorf("%s: What = %q, want %s", name, be.What, c.field)
		}
	}
}

// A walled session name is a typed id, like the ids in waiting_on_human: a free token would be a
// channel across the wall (lead ruling on N7). An unwalled swarm keeps the plain name rule.
func TestWalledStatusSessionNamesAreIds(t *testing.T) {
	e := newEnvWalled(t, "client")
	session := func(name string) json.RawMessage {
		return json.RawMessage(`{"swarm":"client","sessions":[{"name":"` + name + `","state":"idle"}],"waiting_on_human":[],"main_health":"ok","spend":0,"ts":1}`)
	}
	for _, name := range []string{"s1", "s42", "s999999"} {
		if err := pushTo(e, "client", session(name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, name := range []string{"build-1", "review-2", "s0", "s01", "s1234567", "S1", "s", "s1-a", "ob-1"} {
		err := pushTo(e, "client", session(name))
		if be := errWithCode(t, err, bus.CodeInvalid); be.What != "sessions.name" {
			t.Errorf("%s: What = %q", name, be.What)
		}
	}
	crew := json.RawMessage(`{"swarm":"crew","sessions":[{"name":"build-1","state":"idle"}],"waiting_on_human":[],"main_health":"ok","spend":0,"ts":1}`)
	if err := pushTo(e, "crew", crew); err != nil {
		t.Errorf("unwalled: %v", err)
	}
}

// encoding/json matches struct fields without regard to case, so a second spelling of a key would
// reach the struct while the digit check read the first: the keys must be exactly the six, spelled so.
func TestStatusRefusesCaseVariantKeys(t *testing.T) {
	for name, doc := range map[string]string{
		"spend twice":       `{"swarm":"client","sessions":[],"waiting_on_human":[],"main_health":"ok","spend":1,"Spend":123456789012345,"ts":1}`,
		"main_health twice": `{"swarm":"client","sessions":[],"waiting_on_human":[],"main_health":"ok","Main_Health":"payroll-outage","spend":1,"ts":1}`,
		"only SPEND":        `{"swarm":"client","sessions":[],"waiting_on_human":[],"main_health":"ok","SPEND":1,"ts":1}`,
		"an extra key":      `{"swarm":"client","sessions":[],"waiting_on_human":[],"main_health":"ok","spend":1,"ts":1,"note":"x"}`,
	} {
		for _, swarm := range []string{"client", "crew"} {
			e := newEnvWalled(t, "client")
			d := json.RawMessage(strings.Replace(doc, `"client"`, `"`+swarm+`"`, 1))
			err := pushTo(e, swarm, d)
			if be := errWithCode(t, err, bus.CodeInvalid); be.What != "status" {
				t.Errorf("%s in %s: What = %q, want status", name, swarm, be.What)
			}
			if st, _ := e.b.SwarmStatus(context.Background(), duet, ""); len(st) != 0 {
				t.Errorf("%s in %s: stored %+v", name, swarm, st)
			}
		}
	}
}
