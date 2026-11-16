package bus_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// A walled approval's text is held to the go/hold grammar, and the word must agree with the
// decision: go with approve, hold with reject.
func TestWalledApprovalTextOnlyGoOrHold(t *testing.T) {
	e := newEnvWalled(t, "client")
	rel := e.post(t, walledNode, walledRelease()).Item
	mrg := e.post(t, walledNode, walledMerge()).Item
	s := e.snap(t, rel.ID)
	for _, c := range []struct {
		name string
		dec  bus.Decision
		text string
	}{
		{"prose", bus.DecisionApprove, "ship the fix for the client"},
		{"trailing words", bus.DecisionApprove, "go sl:12 now"},
		{"no number", bus.DecisionApprove, "go sl:"},
		{"hold with approve", bus.DecisionApprove, "hold sl:12"},
		{"go with reject", bus.DecisionReject, "go sl:12"},
		{"reject prose", bus.DecisionReject, "no, not this one"},
	} {
		in := approveIn(rel)
		in.Decision, in.Text = c.dec, c.text
		_, err := e.b.RecordApproval(ctx, passkey, in)
		if be := errWithCode(t, err, bus.CodeInvalid); be.What != "text" {
			t.Errorf("%s: What = %q, want text", c.name, be.What)
		}
	}
	e.unchanged(t, "walled approval prose", s)
	in := approveIn(rel)
	in.Text = "go sl:12"
	if r, err := e.b.RecordApproval(ctx, passkey, in); err != nil || r.Text != "go sl:12" {
		t.Errorf("go sl:12 with approve = %+v, %v", r, err)
	}
	in = approveIn(mrg)
	in.Decision, in.Text = bus.DecisionReject, "hold sl:13"
	if r, err := e.b.RecordApproval(ctx, passkey, in); err != nil || r.Decision != bus.DecisionReject {
		t.Errorf("hold sl:13 with reject = %+v, %v", r, err)
	}
}

var badSourceMessages = []string{strings.Repeat("a", 65), "bad\xffutf8", "esc\x1b[31m", "nul\x00", "has space", "owner said: tell the client", "tab\t", "new\nline"}

type srcEnv struct {
	name string
	e    *env
	q    bus.Item
	text string
	wall bool
}

// fresh posts another open question in the env's swarm.
func (s srcEnv) fresh(t *testing.T, i int) bus.Item {
	t.Helper()
	if s.wall {
		d := typed(strconv.Itoa(20 + i))
		d.Kind = bus.KindQuestion
		return s.e.post(t, walledNode, d).Item
	}
	return s.e.post(t, node("ci-1", "crew"), askQuestion("gq"+strconv.Itoa(i))).Item
}

// SourceMessage is untrusted data from Duet: optional, and when present an id token.
func TestSourceMessageRules(t *testing.T) {
	e := newEnv(t)
	w := newEnvWalled(t, "client")
	wd := typed("12")
	wd.Kind = bus.KindQuestion
	wq := w.post(t, walledNode, wd).Item
	for _, env := range []srcEnv{{"crew", e, e.questionItem(t), "use 8080", false}, {"walled", w, wq, "go sl:12", true}} {
		s := env.e.snap(t, env.q.ID)
		for _, sm := range badSourceMessages {
			in := answerIn(env.q)
			in.Text, in.SourceMessage = env.text, sm
			_, err := env.e.b.RecordAnswer(ctx, duet, in)
			if be := errWithCode(t, err, bus.CodeInvalid); be.What != "source_message" {
				t.Errorf("%s answer %q: What = %q", env.name, sm, be.What)
			}
		}
		env.e.unchanged(t, env.name+" refused source messages", s)
		for i, sm := range []string{"", "msg-9", "m.1:x-2", "a_b", strings.Repeat("a", 64)} {
			q := env.fresh(t, i) // a retry on the first item would return its record, so each id gets an item
			in := answerIn(q)
			in.Text, in.SourceMessage = env.text, sm
			r, err := env.e.b.RecordAnswer(ctx, duet, in)
			if err != nil || r.SourceMessage != sm {
				t.Errorf("%s answer %q = %+v, %v", env.name, sm, r, err)
			}
		}
	}
}

func TestApprovalSourceMessageRules(t *testing.T) {
	e := newEnv(t)
	a := e.approvalItem(t)
	s := e.snap(t, a.ID)
	for _, sm := range badSourceMessages {
		in := approveIn(a)
		in.SourceMessage = sm
		_, err := e.b.RecordApproval(ctx, passkey, in)
		if be := errWithCode(t, err, bus.CodeInvalid); be.What != "source_message" {
			t.Errorf("approval %q: What = %q", sm, be.What)
		}
	}
	e.unchanged(t, "refused approval source messages", s)
	in := approveIn(a)
	in.SourceMessage = "msg-1"
	if r, err := e.b.RecordApproval(ctx, passkey, in); err != nil || r.SourceMessage != "msg-1" {
		t.Errorf("approval with a source message = %+v, %v", r, err)
	}
}

func TestDeskWithoutNameIsForbidden(t *testing.T) {
	e := newEnv(t)
	q := e.questionItem(t)
	s := e.snap(t, q.ID)
	_, err := e.b.RecordAnswer(ctx, bus.Principal{Kind: bus.PrincipalDesk}, answerIn(q))
	wantCode(t, err, bus.CodeForbidden)
	e.unchanged(t, "unnamed desk", s)
}

// The convention Task 7 reads events by: an event's Created equals the Recorded of the row it
// records, for answers, approvals and relays as well as for answer_superseded (see the db test).
func TestRecordedEventsMatchRowRecordedTime(t *testing.T) {
	e := newEnv(t)
	q, a := e.questionItem(t), e.approvalItem(t)
	ans, err := e.b.RecordAnswer(ctx, duet, answerIn(q))
	if err != nil {
		t.Fatal(err)
	}
	app, err := e.b.RecordApproval(ctx, passkey, approveIn(a))
	if err != nil {
		t.Fatal(err)
	}
	rel, err := e.b.RelayWords(ctx, duet, relayIn(q))
	if err != nil {
		t.Fatal(err)
	}
	want := map[bus.EventType]time.Time{bus.EventAnswerRecorded: ans.Recorded, bus.EventApprovalRecorded: app.Recorded, bus.EventWordsRelayed: rel.Recorded}
	for _, ev := range e.events(t) {
		if w, ok := want[ev.Type]; ok {
			if !ev.Created.Equal(w) {
				t.Errorf("%s: Created %v, row Recorded %v", ev.Type, ev.Created, w)
			}
			delete(want, ev.Type)
		}
	}
	if len(want) != 0 {
		t.Errorf("events missing: %v", want)
	}
}
