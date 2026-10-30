package pagerduty

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

	"github.com/Exploded-views/yo-human/internal/channel"
)

const fakeKey = "ROUTINGKEY-FAKE-1"

type recorded struct {
	Method, Path, UA, CT string
	Body                 []byte
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

// newServer starts a TLS server that records requests and answers through h (a PagerDuty 202 when nil).
func newServer(t *testing.T, h http.HandlerFunc) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.reqs = append(f.reqs, recorded{r.Method, r.URL.EscapedPath(), r.UserAgent(), r.Header.Get("Content-Type"), b})
		f.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"status":"success","message":"Event processed","dedup_key":"k"}`)
	}))
	t.Cleanup(f.Close)
	return f
}

func build(t *testing.T, f *fakeServer) channel.Channel {
	t.Helper()
	c, err := newWithClient(channel.Instance{Name: "pager", Type: "pagerduty",
		Settings: map[string]any{"events_url": f.URL + "/v2/enqueue"},
		Secrets:  map[string]string{"routing_key": fakeKey}}, "9.9.9", f.Client())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return c
}

func msg() channel.Message {
	return channel.Message{ID: "01FAKEID", Level: "page", Title: "Deploy blocked", Ask: "approve the rollout",
		Body: "details here", Link: "https://example.com/pr/1", Project: "proj", Agent: "agent", DedupKey: "dk-1",
		Actions: []channel.Action{{Label: "Open", URL: "https://example.com/a"}, {Label: "Logs", URL: "http://example.com/l"}}}
}

func decode(t *testing.T, r recorded) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body, &m); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	return m
}

func send(t *testing.T, m channel.Message, i channel.Interruption) map[string]any {
	t.Helper()
	f := newServer(t, nil)
	if _, err := build(t, f).Send(context.Background(), m, channel.SendSpec{Interruption: i}); err != nil {
		t.Fatal(err)
	}
	return decode(t, f.all()[0])
}

func TestTriggerShape(t *testing.T) {
	f := newServer(t, nil)
	rc, err := build(t, f).Send(context.Background(), msg(), channel.SendSpec{Instance: "pager", Interruption: channel.TimeSensitive})
	if err != nil {
		t.Fatal(err)
	}
	if rc != (channel.Receipt{Instance: "pager", ProviderID: "dk-1"}) {
		t.Errorf("receipt = %+v", rc)
	}
	reqs := f.all()
	if len(reqs) != 1 {
		t.Fatalf("%d requests, want 1", len(reqs))
	}
	r := reqs[0]
	head := recorded{Method: r.Method, Path: r.Path, CT: r.CT, UA: r.UA}
	if !reflect.DeepEqual(head, recorded{Method: "POST", Path: "/v2/enqueue", CT: "application/json", UA: "yo-human/9.9.9"}) {
		t.Errorf("request head = %+v", head)
	}
	want := map[string]any{
		"routing_key": fakeKey, "event_action": "trigger", "dedup_key": "dk-1",
		"payload": map[string]any{
			"summary": "Deploy blocked", "source": "proj", "severity": "error", "component": "agent",
			"custom_details": map[string]any{"ask": "approve the rollout", "body": "details here",
				"message_id": "01FAKEID", "level": "page"},
		},
		"links": []any{
			map[string]any{"href": "https://example.com/pr/1"},
			map[string]any{"href": "https://example.com/a", "text": "Open"},
			map[string]any{"href": "http://example.com/l", "text": "Logs"},
		},
	}
	if got := decode(t, r); !reflect.DeepEqual(got, want) {
		t.Errorf("body =\n%v\nwant\n%v", got, want)
	}
}

func TestSeverities(t *testing.T) {
	want := map[channel.Interruption]string{channel.Passive: "info", channel.Active: "warning",
		channel.TimeSensitive: "error", channel.Critical: "critical"}
	for in, sev := range want {
		p := send(t, msg(), in)["payload"].(map[string]any)
		if p["severity"] != sev {
			t.Errorf("%s: severity = %v, want %s", in, p["severity"], sev)
		}
	}
}

func TestDefaultsAndOmissions(t *testing.T) {
	m := channel.Message{ID: "01FAKEID", Title: "t"}
	got := send(t, m, channel.Active)
	p := got["payload"].(map[string]any)
	if got["dedup_key"] != "01FAKEID" || p["source"] != "yo-human" {
		t.Errorf("dedup_key=%v source=%v", got["dedup_key"], p["source"])
	}
	if _, ok := p["component"]; ok {
		t.Error("empty component sent")
	}
	if _, ok := got["links"]; ok {
		t.Error("empty links sent")
	}
}

func TestSummaryTruncatedTo1024(t *testing.T) {
	m := msg()
	m.Title = strings.Repeat("é", 800)
	s := send(t, m, channel.Active)["payload"].(map[string]any)["summary"].(string)
	if len(s) > 1024 || !strings.HasSuffix(s, truncMarker) {
		t.Errorf("summary len %d suffix %q", len(s), s[max(0, len(s)-20):])
	}
	m.Title = strings.Repeat("a", 1024)
	if s := send(t, m, channel.Active)["payload"].(map[string]any)["summary"].(string); len(s) != 1024 || strings.HasSuffix(s, truncMarker) {
		t.Errorf("1024-byte summary altered: len %d", len(s))
	}
}

func TestBodyTruncatedKeepsLinks(t *testing.T) {
	m := msg()
	m.Body = strings.Repeat("x", 1<<20)
	m.Ask = strings.Repeat("y", 1<<20)
	f := newServer(t, nil)
	if _, err := build(t, f).Send(context.Background(), m, channel.SendSpec{Interruption: channel.Active}); err != nil {
		t.Fatal(err)
	}
	r := f.all()[0]
	if len(r.Body) > 512*1000 {
		t.Errorf("payload is %d bytes, over PagerDuty's 512 KB limit", len(r.Body))
	}
	got := decode(t, r)
	cd := got["payload"].(map[string]any)["custom_details"].(map[string]any)
	for _, k := range []string{"body", "ask"} {
		if v := cd[k].(string); len(v) > maxBody || !strings.HasSuffix(v, truncMarker) {
			t.Errorf("%s len %d", k, len(v))
		}
	}
	if len(got["links"].([]any)) != 3 {
		t.Errorf("links = %v", got["links"])
	}
}

func TestResolveShape(t *testing.T) {
	f := newServer(t, nil)
	if err := build(t, f).Resolve(context.Background(), "dk-1", channel.Message{Title: "fixed", Body: "all good"}); err != nil {
		t.Fatal(err)
	}
	reqs := f.all()
	if len(reqs) != 1 || reqs[0].Path != "/v2/enqueue" || reqs[0].UA != "yo-human/9.9.9" {
		t.Fatalf("requests = %+v", reqs)
	}
	want := map[string]any{"routing_key": fakeKey, "event_action": "resolve", "dedup_key": "dk-1"}
	if got := decode(t, reqs[0]); !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

func TestResolveNeedsKey(t *testing.T) {
	f := newServer(t, nil)
	err := build(t, f).Resolve(context.Background(), "", channel.Message{})
	if err == nil || channel.KindOf(err) != channel.PermanentError || len(f.all()) != 0 {
		t.Errorf("err=%v requests=%d", err, len(f.all()))
	}
}

func TestOnlySafeLinksAreSent(t *testing.T) {
	bad := []string{"javascript:alert(1)", "intent://x#Intent;end", "https://u:p" + "@x.example/", "https://:8080/", "ftp://x.example/", "/relative"} // split in the source so the public-repo scrub stays uniform
	for _, u := range bad {
		for _, where := range []string{"link", "action"} {
			m := msg()
			if where == "link" {
				m.Link = u
			} else {
				m.Actions = []channel.Action{{Label: "x", URL: u}}
			}
			f := newServer(t, nil)
			_, err := build(t, f).Send(context.Background(), m, channel.SendSpec{Interruption: channel.Active})
			if err == nil || channel.KindOf(err) != channel.PermanentError || len(f.all()) != 0 {
				t.Errorf("%s %q: err=%v requests=%d", where, u, err, len(f.all()))
				continue
			}
			if s := err.Error(); strings.Contains(s, "x.example") || strings.Contains(s, "alert") || strings.Contains(s, "u:p") {
				t.Errorf("%s %q: error echoes the URL: %q", where, u, s)
			}
		}
	}
}

func TestUnknownInterruptionIsPermanent(t *testing.T) {
	f := newServer(t, nil)
	_, err := build(t, f).Send(context.Background(), msg(), channel.SendSpec{Interruption: "loud"})
	if err == nil || channel.KindOf(err) != channel.PermanentError || len(f.all()) != 0 {
		t.Errorf("err=%v requests=%d", err, len(f.all()))
	}
}
