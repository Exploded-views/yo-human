package ntfy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

const (
	fakeTopic = "TOPIC-FAKE-1"
	fakeToken = "TOK-FAKE-1"
)

type recorded struct {
	Method, Path, Auth, UA, CT string
	Body                       []byte
}

type fakeServer struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []recorded
}

func (f *fakeServer) all() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.reqs...)
}

// newServer starts a TLS server that records requests and answers through h (200 when nil).
func newServer(t *testing.T, h http.HandlerFunc) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.reqs = append(f.reqs, recorded{r.Method, r.URL.EscapedPath(), r.Header.Get("Authorization"),
			r.UserAgent(), r.Header.Get("Content-Type"), b})
		f.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"id":"srv1"}`)
	}))
	t.Cleanup(f.Close)
	return f
}

func build(t *testing.T, f *fakeServer, secrets map[string]string) channel.Channel {
	t.Helper()
	if secrets == nil {
		secrets = map[string]string{"topic": fakeTopic}
	}
	c, err := newWithClient(channel.Instance{Name: "phone", Type: "ntfy",
		Settings: map[string]any{"server": f.URL}, Secrets: secrets}, "9.9.9", f.Client())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return c
}

func msg() channel.Message {
	return channel.Message{ID: "01FAKEID", Title: "Deploy blocked", Ask: "approve the rollout",
		Body: "details here", Link: "https://example.com/pr/1", Project: "proj", Agent: "agent",
		Actions: []channel.Action{{Label: "Open", URL: "https://example.com/a"}}}
}

func decode(t *testing.T, r recorded) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body, &m); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	return m
}

func TestSendJSONShape(t *testing.T) {
	f := newServer(t, nil)
	c := build(t, f, nil)
	rc, err := c.Send(context.Background(), msg(), channel.SendSpec{Instance: "phone",
		Interruption: channel.Active, Options: map[string]any{"tags": []any{"extra"}, "markdown": true}})
	if err != nil {
		t.Fatal(err)
	}
	if rc != (channel.Receipt{Instance: "phone", ProviderID: "01FAKEID"}) {
		t.Errorf("receipt = %+v", rc)
	}
	reqs := f.all()
	if len(reqs) != 1 {
		t.Fatalf("%d requests, want 1", len(reqs))
	}
	r := reqs[0]
	head := recorded{Method: r.Method, Path: r.Path, CT: r.CT, UA: r.UA, Auth: r.Auth}
	if !reflect.DeepEqual(head, recorded{Method: "POST", Path: "/", CT: "application/json", UA: "yo-human/9.9.9"}) {
		t.Errorf("request head = %+v", head)
	}
	checkShape(t, decode(t, r))
}

func checkShape(t *testing.T, got map[string]any) {
	t.Helper()
	want := map[string]any{
		"topic": fakeTopic, "title": "Deploy blocked", "message": "approve the rollout\ndetails here",
		"priority": float64(3), "tags": []any{"extra", "proj", "agent"}, "click": "https://example.com/pr/1",
		"actions":  []any{map[string]any{"action": "view", "label": "Open", "url": "https://example.com/a"}},
		"markdown": true, "sequence_id": "01FAKEID",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body =\n%v\nwant\n%v", got, want)
	}
}

func TestPriorityMapping(t *testing.T) {
	for in, want := range map[channel.Interruption]float64{channel.Passive: 2, channel.Active: 3, channel.TimeSensitive: 4} {
		f := newServer(t, nil)
		if _, err := build(t, f, nil).Send(context.Background(), msg(), channel.SendSpec{Interruption: in}); err != nil {
			t.Fatal(err)
		}
		if got := decode(t, f.all()[0])["priority"]; got != want {
			t.Errorf("%s: priority = %v, want %v", in, got, want)
		}
	}
}

func TestCriticalIsRejectedPermanently(t *testing.T) {
	f := newServer(t, nil)
	_, err := build(t, f, nil).Send(context.Background(), msg(), channel.SendSpec{Interruption: channel.Critical})
	if err == nil || channel.KindOf(err) != channel.PermanentError {
		t.Fatalf("err = %v, want permanent", err)
	}
	if len(f.all()) != 0 {
		t.Error("a critical send reached the server")
	}
}

func TestOptionalFieldsOmitted(t *testing.T) {
	f := newServer(t, nil)
	m := channel.Message{ID: "01FAKEID", Title: "t", Body: "only body"}
	if _, err := build(t, f, nil).Send(context.Background(), m, channel.SendSpec{Interruption: channel.Passive}); err != nil {
		t.Fatal(err)
	}
	got := decode(t, f.all()[0])
	for _, k := range []string{"click", "actions", "markdown", "delay", "tags"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s present, want omitted: %v", k, got[k])
		}
	}
	if got["message"] != "only body" {
		t.Errorf("message = %v", got["message"])
	}
}

func TestScheduledSendHasDelayAndCancelDeletes(t *testing.T) {
	f := newServer(t, nil)
	c := build(t, f, nil)
	at := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	rc, err := c.Send(context.Background(), msg(), channel.SendSpec{Instance: "phone", Interruption: channel.Active, DeliverAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if !rc.Scheduled {
		t.Error("receipt not Scheduled")
	}
	if got := decode(t, f.all()[0])["delay"]; got != strings.TrimSpace(itoa(at.Unix())) {
		t.Errorf("delay = %v, want unix %d", got, at.Unix())
	}
	if err := c.Cancel(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	reqs := f.all()
	if len(reqs) != 2 || reqs[1].Method != "DELETE" || reqs[1].Path != "/"+fakeTopic+"/01FAKEID" {
		t.Errorf("cancel request = %+v", reqs[1:])
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestBearerTokenOnlyWhenSet(t *testing.T) {
	f := newServer(t, nil)
	c := build(t, f, map[string]string{"topic": fakeTopic, "token": fakeToken})
	if _, err := c.Send(context.Background(), msg(), channel.SendSpec{Interruption: channel.Active}); err != nil {
		t.Fatal(err)
	}
	if err := c.Cancel(context.Background(), channel.Receipt{ProviderID: "x"}); err != nil {
		t.Fatal(err)
	}
	for _, r := range f.all() {
		if r.Auth != "Bearer "+fakeToken {
			t.Errorf("%s Authorization = %q", r.Method, r.Auth)
		}
	}
	g := newServer(t, nil)
	if _, err := build(t, g, nil).Send(context.Background(), msg(), channel.SendSpec{Interruption: channel.Active}); err != nil {
		t.Fatal(err)
	}
	if a := g.all()[0].Auth; a != "" {
		t.Errorf("Authorization = %q without a token", a)
	}
}

func TestBodyTruncatedWithMarker(t *testing.T) {
	f := newServer(t, nil)
	m := msg()
	m.Ask = ""
	m.Body = "x" + strings.Repeat("é", 3000) // runes start at odd offsets: the cut lands inside one
	if _, err := build(t, f, nil).Send(context.Background(), m, channel.SendSpec{Interruption: channel.Active}); err != nil {
		t.Fatal(err)
	}
	got := decode(t, f.all()[0])["message"].(string)
	if len(got) > 4000 || !strings.HasSuffix(got, "…[truncated]") {
		t.Errorf("len=%d suffix=%q", len(got), got[len(got)-14:])
	}
	if len(got) < 3900 {
		t.Errorf("truncated too far: %d", len(got))
	}
	if strings.ContainsRune(got, '�') {
		t.Error("cut inside a rune")
	}
	m.Body = strings.Repeat("a", 4000)
	f2 := newServer(t, nil)
	if _, err := build(t, f2, nil).Send(context.Background(), m, channel.SendSpec{Interruption: channel.Active}); err != nil {
		t.Fatal(err)
	}
	if got := decode(t, f2.all()[0])["message"].(string); got != m.Body {
		t.Errorf("a body of exactly 4000 bytes was altered (len %d)", len(got))
	}
}

func TestNearDeliveryTimeSendsAtOnce(t *testing.T) {
	f := newServer(t, nil)
	rc, err := build(t, f, nil).Send(context.Background(), msg(),
		channel.SendSpec{Interruption: channel.Active, DeliverAt: time.Now().Add(2 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decode(t, f.all()[0])["delay"]; ok || rc.Scheduled {
		t.Error("a delay under ntfy's 10 second minimum was sent")
	}
}

func TestCancelWithoutProviderIDIsPermanent(t *testing.T) {
	f := newServer(t, nil)
	if err := build(t, f, nil).Cancel(context.Background(), channel.Receipt{}); err == nil || channel.KindOf(err) != channel.PermanentError {
		t.Errorf("err = %v", err)
	}
}

func TestAsciiBodyTruncatesToExactlyTheLimit(t *testing.T) {
	f := newServer(t, nil)
	m := msg()
	m.Ask, m.Body = "", strings.Repeat("a", 5000)
	if _, err := build(t, f, nil).Send(context.Background(), m, channel.SendSpec{Interruption: channel.Active}); err != nil {
		t.Fatal(err)
	}
	if got := decode(t, f.all()[0])["message"].(string); len(got) != 4000 || !strings.HasSuffix(got, "…[truncated]") {
		t.Errorf("len = %d", len(got))
	}
}

func TestOnlyHTTPLinksReachThePhone(t *testing.T) {
	bad := []string{"intent://x#Intent;scheme=evil;end", "javascript:alert(1)", "tel:+15550100", "ftp://x.example/f", "//x.example", "https:///nohost", "https://:8080/", "https://u" + ":p" + "@x.example/"} // split in the source so the public-repo scrub stays uniform
	for i, u := range bad {
		withLink, withAction := msg(), msg()
		withLink.Link = u
		withAction.Link, withAction.Actions = "", []channel.Action{{Label: "Go", URL: u}}
		for _, m := range []channel.Message{withLink, withAction} {
			f := newServer(t, nil)
			_, err := build(t, f, nil).Send(context.Background(), m, channel.SendSpec{Interruption: channel.Active})
			if err == nil || channel.KindOf(err) != channel.PermanentError || strings.Contains(err.Error(), u) || len(f.all()) != 0 {
				t.Errorf("#%d: err=%v requests=%d", i, err, len(f.all()))
			}
		}
	}
	empty := msg()
	empty.Link, empty.Actions = "", []channel.Action{{Label: "Go"}}
	if _, err := build(t, newServer(t, nil), nil).Send(context.Background(), empty, channel.SendSpec{Interruption: channel.Active}); err == nil {
		t.Error("an action with no URL was sent")
	}
}

func TestDeliveryMoreThanThreeDaysOutIsPermanent(t *testing.T) {
	f := newServer(t, nil)
	c := build(t, f, nil)
	_, err := c.Send(context.Background(), msg(), channel.SendSpec{Interruption: channel.Active, DeliverAt: time.Now().Add(73 * time.Hour)})
	if err == nil || channel.KindOf(err) != channel.PermanentError || !strings.Contains(err.Error(), "3 days") {
		t.Errorf("err = %v", err)
	}
	if len(f.all()) != 0 {
		t.Error("a request was made")
	}
	if _, err := c.Send(context.Background(), msg(), channel.SendSpec{Interruption: channel.Active, DeliverAt: time.Now().Add(71 * time.Hour)}); err != nil {
		t.Errorf("71h: %v", err)
	}
}

func TestAskAloneIsTheMessage(t *testing.T) {
	f := newServer(t, nil)
	m := msg()
	m.Body = ""
	if _, err := build(t, f, nil).Send(context.Background(), m, channel.SendSpec{Interruption: channel.Active}); err != nil {
		t.Fatal(err)
	}
	if got := decode(t, f.all()[0])["message"]; got != "approve the rollout" {
		t.Errorf("message = %v", got)
	}
}

// A scheduled message that is already gone (a cancel re-run after a crash, or a message that was
// delivered or deleted) is cancelled: 404 is success, and no other failure is.
func TestCancelFailuresKeepTheirKinds(t *testing.T) {
	for status, want := range map[int]channel.ErrorKind{http.StatusForbidden: channel.PermanentError, http.StatusInternalServerError: channel.TransientError} {
		f := newServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
		err := build(t, f, nil).Cancel(context.Background(), channel.Receipt{ProviderID: "01FAKEID"})
		if err == nil || channel.KindOf(err) != want {
			t.Errorf("cancel answered %d: err %v; want a %v error", status, err, want)
		}
	}
}

func TestCancelThatAnswers404IsPermanentAndSaysThePageMayStillFire(t *testing.T) {
	f := newServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	c := build(t, f, nil)
	err := c.Cancel(context.Background(), channel.Receipt{ProviderID: "01FAKEID"})
	if err == nil || channel.KindOf(err) != channel.PermanentError {
		t.Fatalf("cancel answered 404: err %v; want a permanent error (an old server or a proxy may not route DELETE)", err)
	}
	const why = "the scheduled send may already be gone, or this ntfy server cannot cancel scheduled messages (needs v2.16 or later); the human may still be paged"
	if !strings.Contains(err.Error(), why) || !strings.Contains(err.Error(), "404") {
		t.Errorf("cancel answered 404: %q; want the status and %q", err, why)
	}
	if _, err := c.Send(context.Background(), msg(), channel.SendSpec{Interruption: channel.Active}); err == nil || channel.KindOf(err) != channel.PermanentError || strings.Contains(err.Error(), "v2.16") {
		t.Errorf("a 404 on a send = %v; want a plain permanent failure", err)
	}
}
