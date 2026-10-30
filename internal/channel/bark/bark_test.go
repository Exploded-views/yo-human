package bark

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

const fakeKey = "DEVICEKEY-FAKE-1"

const okReply = `{"code":200,"message":"success","timestamp":1}`

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

// newServer starts a TLS server that records requests and answers through h (a Bark success when nil).
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
		_, _ = io.WriteString(w, okReply)
	}))
	t.Cleanup(f.Close)
	return f
}

func build(t *testing.T, f *fakeServer) channel.Channel {
	t.Helper()
	c, err := newWithClient(channel.Instance{Name: "phone", Type: "bark",
		Settings: map[string]any{"server": f.URL}, Secrets: map[string]string{"device_key": fakeKey}}, "9.9.9", f.Client())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return c
}

func msg() channel.Message {
	return channel.Message{ID: "01FAKEID", Title: "Deploy blocked", Ask: "approve the rollout",
		Body: "details here", Link: "https://example.com/pr/1", Project: "proj", Agent: "agent",
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

func send(t *testing.T, m channel.Message, s channel.SendSpec) map[string]any {
	t.Helper()
	f := newServer(t, nil)
	if _, err := build(t, f).Send(context.Background(), m, s); err != nil {
		t.Fatal(err)
	}
	return decode(t, f.all()[0])
}

func TestSendJSONShape(t *testing.T) {
	f := newServer(t, nil)
	rc, err := build(t, f).Send(context.Background(), msg(), channel.SendSpec{Instance: "phone",
		Interruption: channel.Active, Options: map[string]any{"sound": "minuet", "call": true, "volume": 7}})
	if err != nil {
		t.Fatal(err)
	}
	if rc != (channel.Receipt{Instance: "phone"}) {
		t.Errorf("receipt = %+v", rc)
	}
	reqs := f.all()
	if len(reqs) != 1 {
		t.Fatalf("%d requests, want 1", len(reqs))
	}
	r := reqs[0]
	head := recorded{Method: r.Method, Path: r.Path, CT: r.CT, UA: r.UA}
	if !reflect.DeepEqual(head, recorded{Method: "POST", Path: "/push", CT: "application/json", UA: "yo-human/9.9.9"}) {
		t.Errorf("request head = %+v", head)
	}
	want := map[string]any{
		"device_key": fakeKey, "title": "Deploy blocked", "subtitle": "approve the rollout",
		"body":  "details here\nOpen: https://example.com/a\nLogs: http://example.com/l",
		"level": "active", "group": "proj", "url": "https://example.com/pr/1",
		"sound": "minuet", "call": "1", "volume": "7",
	}
	if got := decode(t, r); !reflect.DeepEqual(got, want) {
		t.Errorf("body =\n%v\nwant\n%v", got, want)
	}
}

func TestInterruptionLevels(t *testing.T) {
	want := map[channel.Interruption]string{channel.Passive: "passive", channel.Active: "active",
		channel.TimeSensitive: "timeSensitive", channel.Critical: "critical"}
	for in, level := range want {
		if got := send(t, msg(), channel.SendSpec{Interruption: in})["level"]; got != level {
			t.Errorf("%s: level = %v, want %s", in, got, level)
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

func TestOptionalFieldsOmitted(t *testing.T) {
	m := channel.Message{ID: "01FAKEID", Title: "t", Body: "only body"}
	got := send(t, m, channel.SendSpec{Interruption: channel.Passive, Options: map[string]any{"call": false}})
	for _, k := range []string{"subtitle", "group", "url", "sound", "call", "volume"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s present, want omitted: %v", k, got[k])
		}
	}
	if got["body"] != "only body" {
		t.Errorf("body = %v", got["body"])
	}
}

func TestVolumeForms(t *testing.T) {
	for in, want := range map[any]string{0: "0", 10: "10", 7.0: "7", int64(3): "3"} {
		got := send(t, msg(), channel.SendSpec{Interruption: channel.Active, Options: map[string]any{"volume": in}})
		if got["volume"] != want {
			t.Errorf("volume %v sent as %v, want %q", in, got["volume"], want)
		}
	}
}

func TestBodyTruncatedWithMarker(t *testing.T) {
	m := msg()
	m.Actions, m.Body = nil, "x"+strings.Repeat("é", 3000) // the cut lands inside a rune
	got := send(t, m, channel.SendSpec{Interruption: channel.Active})["body"].(string)
	if len(got) > 4000 || len(got) < 3900 || !strings.HasSuffix(got, "…[truncated]") || strings.ContainsRune(got, '�') {
		t.Errorf("len=%d tail=%q", len(got), got[len(got)-14:])
	}
	m.Body = strings.Repeat("a", 4000)
	if got := send(t, m, channel.SendSpec{Interruption: channel.Active})["body"].(string); got != m.Body {
		t.Errorf("a body of exactly 4000 bytes was altered (len %d)", len(got))
	}
	m.Body = strings.Repeat("a", 5000)
	if got := send(t, m, channel.SendSpec{Interruption: channel.Active})["body"].(string); len(got) != 4000 {
		t.Errorf("len = %d, want 4000", len(got))
	}
}

func TestOnlyHTTPLinksReachThePhone(t *testing.T) {
	bad := []string{"intent://x#Intent;scheme=evil;end", "javascript:alert(1)", "tel:+15550100", "ftp://x.example/f",
		"//x.example", "https:///nohost", "https://:8080/", "https://u" + ":p" + "@x.example/"} // split in the source so the public-repo scrub stays uniform
	for i, u := range bad {
		withLink, withAction := msg(), msg()
		withLink.Link = u
		withAction.Link, withAction.Actions = "", []channel.Action{{Label: "Go", URL: u}}
		for _, m := range []channel.Message{withLink, withAction} {
			f := newServer(t, nil)
			_, err := build(t, f).Send(context.Background(), m, channel.SendSpec{Interruption: channel.Active})
			if err == nil || channel.KindOf(err) != channel.PermanentError || strings.Contains(err.Error(), u) || len(f.all()) != 0 {
				t.Errorf("#%d: err=%v requests=%d", i, err, len(f.all()))
			}
		}
	}
	empty := msg()
	empty.Link, empty.Actions = "", []channel.Action{{Label: "Go"}}
	if _, err := build(t, newServer(t, nil)).Send(context.Background(), empty, channel.SendSpec{Interruption: channel.Active}); err == nil {
		t.Error("an action with no URL was sent")
	}
}

func TestLongBodyKeepsEveryAction(t *testing.T) {
	m := msg()
	m.Body = strings.Repeat("x", 6000)
	got := send(t, m, channel.SendSpec{Interruption: channel.Active})["body"].(string)
	suffix := "Open: https://example.com/a\nLogs: http://example.com/l"
	if len(got) > 4000 || !strings.HasSuffix(got, "\n"+suffix) || !strings.Contains(got, "…[truncated]\n") {
		t.Errorf("len=%d tail=%q", len(got), got[max(0, len(got)-90):])
	}
	// a body that leaves no room: the whole string is cut instead
	m.Actions = []channel.Action{{Label: "Big", URL: "https://example.com/" + strings.Repeat("p", 4500)}}
	got = send(t, m, channel.SendSpec{Interruption: channel.Active})["body"].(string)
	if len(got) > 4000 || !strings.HasSuffix(got, "…[truncated]") {
		t.Errorf("oversized suffix: len=%d", len(got))
	}
	m.Body = ""
	m.Actions = []channel.Action{{Label: "A", URL: "https://example.com/" + strings.Repeat("p", 2500)},
		{Label: "B", URL: "https://example.com/" + strings.Repeat("q", 2500)}}
	if got := send(t, m, channel.SendSpec{Interruption: channel.Active})["body"].(string); len(got) > 4000 || !strings.HasSuffix(got, "…[truncated]") {
		t.Errorf("actions only, oversized: len=%d", len(got))
	}
	m.Actions = []channel.Action{{Label: "Only", URL: "https://example.com/o"}}
	if got := send(t, m, channel.SendSpec{Interruption: channel.Active})["body"]; got != "Only: https://example.com/o" {
		t.Errorf("actions only: %q", got)
	}
}
