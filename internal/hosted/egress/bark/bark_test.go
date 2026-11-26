package bark_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/egress/bark"
	"github.com/Exploded-views/yo-human/internal/hosted/notify"
)

const (
	user   = "BARK-USER-FAKE"
	pass   = "BARK-PASS-FAKE"
	device = "DEVICE-KEY-FAKE"
)

// fakeBark records the requests it gets and answers with the status (and headers) set.
type fakeBark struct {
	*httptest.Server
	mu      sync.Mutex
	status  int
	headers map[string]string
	reqs    []seen
}

type seen struct {
	method, path, user, pass, ctype string
	body                            map[string]any
}

func newFake(t *testing.T) *fakeBark {
	t.Helper()
	f := &fakeBark{status: 200}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		u, p, _ := r.BasicAuth()
		f.mu.Lock()
		f.reqs = append(f.reqs, seen{r.Method, r.URL.Path, u, p, r.Header.Get("Content-Type"), body})
		status, headers := f.status, f.headers
		f.mu.Unlock()
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"code":`+"1"+`,"message":"`+pass+` `+device+`"}`) // a hostile body that echoes secrets
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeBark) set(status int, headers map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.headers = status, headers
}

func (f *fakeBark) client() *bark.Client {
	return &bark.Client{BaseURL: f.URL, User: user, Password: pass, DeviceKey: device}
}

func (f *fakeBark) last(t *testing.T) seen {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reqs) == 0 {
		t.Fatal("Bark got no request")
	}
	return f.reqs[len(f.reqs)-1]
}

func TestBarkRequestShape(t *testing.T) {
	f := newFake(t)
	err := f.client().Send(context.Background(), notify.Message{ID: "yh-7", Title: "yo-human", Body: "3 new, 4 waiting for Claude", Group: "yo-human", Level: notify.LevelActive})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	r := f.last(t)
	if r.method != "POST" || r.path != "/push" || r.user != user || r.pass != pass || !strings.HasPrefix(r.ctype, "application/json") {
		t.Errorf("request = %+v, want POST /push with basic auth %s/%s and a JSON body", r, user, pass)
	}
	want := map[string]any{"device_key": device, "title": "yo-human", "body": "3 new, 4 waiting for Claude", "group": "yo-human", "level": "active", "id": "yh-7"}
	if len(r.body) != len(want) {
		t.Errorf("body = %v, want exactly the fields %v", r.body, want)
	}
	for k, v := range want {
		if r.body[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, r.body[k], v)
		}
	}
}

func TestBarkLevelMapping(t *testing.T) {
	f := newFake(t)
	for level, want := range map[notify.Level]string{notify.LevelPassive: "passive", notify.LevelActive: "active",
		notify.LevelTimeSensitive: "timeSensitive", notify.LevelCritical: "critical"} {
		if err := f.client().Send(context.Background(), notify.Message{Title: "t", Body: "b", Group: "g", Level: level}); err != nil {
			t.Fatal(err)
		}
		if got := f.last(t).body["level"]; got != want {
			t.Errorf("level %s sent as %v, want %s", level, got, want)
		}
	}
}

func TestBarkSendsIDOnlyWhenSet(t *testing.T) {
	f := newFake(t)
	if err := f.client().Send(context.Background(), notify.Message{Title: "t", Body: "b", Group: "g", Level: notify.LevelActive}); err != nil {
		t.Fatal(err)
	}
	if _, has := f.last(t).body["id"]; has {
		t.Errorf("body has an id for a message with none: %v", f.last(t).body)
	}
}

func TestBarkNameAndLevels(t *testing.T) {
	c := &bark.Client{}
	if c.Name() != "bark" {
		t.Errorf("Name = %q", c.Name())
	}
	got := map[notify.Level]bool{}
	for _, l := range c.Levels() {
		got[l] = true
	}
	for _, l := range []notify.Level{notify.LevelPassive, notify.LevelActive, notify.LevelTimeSensitive, notify.LevelCritical} {
		if !got[l] {
			t.Errorf("Levels lacks %s", l)
		}
	}
}

func TestBarkErrorsCarryNoSecrets(t *testing.T) {
	f := newFake(t)
	f.set(500, nil)
	err := f.client().Send(context.Background(), notify.Message{Title: "t", Body: "b", Group: "g", Level: notify.LevelActive})
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v, want one naming status 500", err)
	}
	for _, secret := range []string{pass, device, user} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error %q carries a secret", err)
		}
	}
	f.Close() // a refused connection: the transport's own error must not carry them either
	err = f.client().Send(context.Background(), notify.Message{Title: "t", Body: "b", Group: "g", Level: notify.LevelActive})
	if err == nil {
		t.Fatal("a closed server gave no error")
	}
	for _, secret := range []string{pass, device} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("transport error %q carries a secret", err)
		}
	}
}

func sendOne(f *fakeBark) error {
	return f.client().Send(context.Background(), notify.Message{Title: "t", Body: "b", Group: "g", Level: notify.LevelActive})
}

func TestBarkErrorClasses(t *testing.T) {
	f := newFake(t)
	for _, c := range []struct {
		status       int
		perm, broken bool
	}{{400, true, false}, {404, true, false}, {413, true, false}, {401, false, true}, {403, false, true},
		{408, false, false}, {429, false, false}, {500, false, false}, {503, false, false}} {
		f.set(c.status, nil)
		err := sendOne(f)
		if err == nil || notify.IsPermanent(err) != c.perm || notify.IsBroken(err) != c.broken || notify.Status(err) != c.status {
			t.Errorf("%d: err=%v permanent=%v broken=%v status=%d, want permanent=%v broken=%v", c.status, err,
				notify.IsPermanent(err), notify.IsBroken(err), notify.Status(err), c.perm, c.broken)
		}
	}
}

func TestBarkSuccessAndClosedServer(t *testing.T) {
	f := newFake(t)
	for _, status := range []int{200, 204} {
		f.set(status, nil)
		if err := sendOne(f); err != nil {
			t.Errorf("%d: %v", status, err)
		}
	}
	f.Close()
	if err := sendOne(f); err == nil || notify.IsPermanent(err) || notify.IsBroken(err) {
		t.Errorf("a closed server: %v, want a transient error", err)
	}
}

func TestBarkRetryAfter(t *testing.T) {
	f := newFake(t)
	send := func() error { return sendOne(f) }
	for _, c := range []struct {
		status int
		header string
		want   time.Duration
	}{{429, "120", 120 * time.Second}, {503, "30", 30 * time.Second}, {429, "99999", time.Hour}, {429, "", 0},
		{429, "soon", 0}, {429, "-5", 0}, {500, "60", 0}} {
		hdr := map[string]string{}
		if c.header != "" {
			hdr["Retry-After"] = c.header
		}
		f.set(c.status, hdr)
		if got := notify.RetryAfter(send()); got != c.want {
			t.Errorf("%d Retry-After %q: RetryAfter = %v, want %v", c.status, c.header, got, c.want)
		}
	}
}

func TestBarkHonoursTheContext(t *testing.T) {
	f := newFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := f.client().Send(ctx, notify.Message{Title: "t", Body: "b", Group: "g", Level: notify.LevelActive})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want one wrapping context.Canceled", err)
	}
	if notify.IsPermanent(err) || notify.IsBroken(err) {
		t.Errorf("a cancelled send is classed %v", err)
	}
}

func TestBarkNilHTTPUsesFiveSecondClient(t *testing.T) {
	stall := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-stall:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(stall); srv.Close() })
	c := &bark.Client{BaseURL: srv.URL, User: user, Password: pass, DeviceKey: device}
	start := time.Now()
	err := c.Send(context.Background(), notify.Message{Title: "t", Body: "b", Group: "g", Level: notify.LevelActive})
	if err == nil || time.Since(start) < 4*time.Second || time.Since(start) > 8*time.Second {
		t.Errorf("a stalled server gave %v after %v, want a transient error near 5 s", err, time.Since(start))
	}
	if notify.IsPermanent(err) || notify.IsBroken(err) {
		t.Errorf("a timeout is classed %v", err)
	}
}

func TestBarkTrailingSlashBaseURL(t *testing.T) {
	f := newFake(t)
	c := f.client()
	c.BaseURL += "/"
	if err := c.Send(context.Background(), notify.Message{Title: "t", Body: "b", Group: "g", Level: notify.LevelActive}); err != nil {
		t.Fatal(err)
	}
	if got := f.last(t).path; got != "/push" {
		t.Errorf("path = %q, want /push", got)
	}
}

func TestBarkWithAnUnusableBaseURLIsABrokenChannel(t *testing.T) {
	c := &bark.Client{BaseURL: "http://\x7f" + device, User: user, Password: pass, DeviceKey: device}
	err := c.Send(context.Background(), notify.Message{Title: "t", Body: "b", Group: "g", Level: notify.LevelActive})
	if err == nil || strings.Contains(err.Error(), device) || notify.IsPermanent(err) || !notify.IsBroken(err) {
		t.Errorf("err = %v, want a broken channel (nothing it sends will arrive) that does not echo the URL", err)
	}
}

// A redirect is never followed: a 307 or 308 would re-post the body, device key and all, to
// whatever host the Location names. The channel is misconfigured, so it is broken.
func TestBarkNeverFollowsARedirectAndTreatsItAsBroken(t *testing.T) {
	second := newFake(t)
	for _, code := range []int{301, 302, 303, 307, 308} {
		first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, second.URL+"/push", code)
		}))
		for _, hc := range []*http.Client{nil, {}} { // the package's own client, and one the caller supplied
			c := &bark.Client{BaseURL: first.URL, User: user, Password: pass, DeviceKey: device, HTTP: hc}
			err := c.Send(context.Background(), notify.Message{Title: "t", Body: "b", Group: "g", Level: notify.LevelActive})
			if err == nil || !notify.IsBroken(err) || notify.Status(err) != code {
				t.Errorf("%d: err = %v (broken %v, status %d), want a broken channel with that status", code, err, notify.IsBroken(err), notify.Status(err))
			}
			if err != nil && (strings.Contains(err.Error(), device) || strings.Contains(err.Error(), second.URL)) {
				t.Errorf("%d: the error %q names the key or the second host", code, err)
			}
		}
		first.Close()
	}
	second.mu.Lock()
	defer second.mu.Unlock()
	if len(second.reqs) != 0 {
		t.Errorf("the second host got %d requests, want none: the device key must never be re-sent", len(second.reqs))
	}
}
