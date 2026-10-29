package bus_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
	"github.com/Exploded-views/yo-human/internal/hosted/policy"
)

var t0 = time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)

type env struct {
	b   *bus.Bus
	d   *db.DB
	l   bus.Limits
	now func() time.Time
}

// newEnv is a Bus over a real database in a temp dir, with a clock that moves one second per
// read and a policy that lets a merge to o/r main through as a passkey approval.
func newEnv(t *testing.T) *env { return newEnvWalled(t) }

// newEnvWalled is newEnv with the named swarms behind the client wall.
func newEnvWalled(t *testing.T, walled ...string) *env {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(filepath.Join(dir, "bus.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	n := 0
	now := func() time.Time { n++; return t0.Add(time.Duration(n) * time.Second) }
	l := bus.Limits{
		LinkHosts: []string{"github.com"},
		Policy: policy.Policy{V: 1, Rules: []policy.Rule{
			{Action: "merge", Repo: "o/r", Ref: "main", Mode: policy.Passkey},
			{Action: "release", Repo: "cn/alpha", Ref: "v*", Mode: policy.Passkey},
			{Action: "merge", Repo: "cn/alpha", Ref: "main", Mode: policy.Passkey},
		}},
		WalledSwarms: walled,
	}
	return &env{b: bus.New(d, now, l), d: d, l: l, now: now}
}

func node(name, swarm string) bus.Principal {
	return bus.Principal{Kind: bus.PrincipalNode, Name: name, Swarm: swarm}
}

var duet = bus.Principal{Kind: bus.PrincipalDuet, Name: "duet"}

func notice(handle string) bus.Draft {
	return bus.Draft{Kind: bus.KindNotice, Severity: bus.SeverityNormal, To: "human", Handle: handle, Title: "Build done", Action: bus.ActionNone,
		Urgency: bus.UrgencyDigest}
}

func mergeApproval(handle string) bus.Draft {
	return bus.Draft{
		Kind: bus.KindApproval, Severity: bus.SeverityHigh, To: "human", Handle: handle, Title: "Merge PR 7",
		Action: bus.ActionMerge, Target: "o/r#7", Repo: "o/r", Ref: "7", BaseRef: "main",
		PinnedCommit: strings.Repeat("0a", 20), Urgency: bus.UrgencyDigest,
	}
}

func (e *env) post(t *testing.T, p bus.Principal, d bus.Draft) bus.PostResult {
	t.Helper()
	r, err := e.b.Post(context.Background(), p, d)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	return r
}

func (e *env) events(t *testing.T) []bus.Event {
	t.Helper()
	var out []bus.Event
	err := e.d.Tx(context.Background(), func(x bus.Tx) (err error) {
		out, err = x.EventsSince("", 0, db.MaxEvents)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (e *env) stored(t *testing.T, id string) (it bus.Item, found bool) {
	t.Helper()
	err := e.d.Tx(context.Background(), func(x bus.Tx) (err error) {
		it, found, err = x.ItemByID(id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return it, found
}

func (e *env) messages(t *testing.T, id string) []bus.Message {
	t.Helper()
	var out []bus.Message
	err := e.d.Tx(context.Background(), func(x bus.Tx) (err error) {
		out, err = x.Messages(id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func wantCode(t *testing.T, err error, code bus.Code) {
	t.Helper()
	_ = errWithCode(t, err, code)
}

func errWithCode(t *testing.T, err error, code bus.Code) *bus.Error {
	t.Helper()
	be, ok := bus.AsError(err)
	if !ok {
		t.Fatalf("err = %v (%T), want a *bus.Error with code %s", err, err, code)
	}
	if be.Code != code {
		t.Fatalf("code = %s (%s), want %s", be.Code, be.What, code)
	}
	return be
}

// same fails the test unless got and want are deeply equal; it is how tests compare several
// fields at once without piling up conditions.
func same(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %+v, want %+v", what, got, want)
	}
}

func types(evs []bus.Event) []bus.EventType {
	var out []bus.EventType
	for _, e := range evs {
		out = append(out, e.Type)
	}
	return out
}
