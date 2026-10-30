package pagerduty

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

var active = channel.SendSpec{Interruption: channel.Active}

// status answers with an HTTP status and a body that echoes what a sloppy server might.
func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, `{"status":"invalid event","message":"Event object is invalid for `+fakeKey+`","errors":["bad"]}`)
	}
}

func TestErrorKinds(t *testing.T) {
	cases := map[int]channel.ErrorKind{
		400: channel.PermanentError, 401: channel.PermanentError, 403: channel.PermanentError,
		404: channel.PermanentError, 429: channel.TransientError, 500: channel.TransientError,
		503: channel.TransientError, 408: channel.TransientError, 302: channel.PermanentError,
		200: channel.TransientError,
	}
	for code, want := range cases {
		_, err := build(t, newServer(t, status(code))).Send(context.Background(), msg(), active)
		if err == nil || channel.KindOf(err) != want {
			t.Errorf("send %d: err=%v kind=%v, want %v", code, err, channel.KindOf(err), want)
		}
		err = build(t, newServer(t, status(code))).Resolve(context.Background(), "k", channel.Message{})
		if err == nil || channel.KindOf(err) != want {
			t.Errorf("resolve %d: err=%v kind=%v, want %v", code, err, channel.KindOf(err), want)
		}
	}
	if _, err := build(t, newServer(t, nil)).Send(context.Background(), msg(), active); err != nil {
		t.Errorf("202: %v", err)
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	other := newServer(t, nil)
	f := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/v2/enqueue", http.StatusTemporaryRedirect)
	})
	_, err := build(t, f).Send(context.Background(), msg(), active)
	if err == nil || channel.KindOf(err) != channel.PermanentError {
		t.Errorf("err=%v kind=%v, want permanent", err, channel.KindOf(err))
	}
	if n := len(other.all()); n != 0 {
		t.Errorf("redirect target got %d requests (the routing key would leak)", n)
	}
}

func TestNoSecretInErrors(t *testing.T) {
	check := func(label string, err error) {
		t.Helper()
		if err == nil {
			t.Errorf("%s: no error", label)
			return
		}
		for e := error(err); e != nil; e = errors.Unwrap(e) {
			if s := e.Error(); strings.Contains(s, "FAKE") || strings.Contains(s, "127.0.0.1") || strings.Contains(s, "invalid") {
				t.Errorf("%s: secret, address or body in error text %q", label, s)
			}
		}
	}
	d := newServer(t, nil)
	dc := build(t, d)
	d.Close()
	_, err := dc.Send(context.Background(), msg(), active)
	check("dial", err)
	check("dial resolve", dc.Resolve(context.Background(), "k", channel.Message{}))
	for _, code := range []int{400, 401, 429, 503, 200} {
		_, err := build(t, newServer(t, status(code))).Send(context.Background(), msg(), active)
		check("status", err)
		check("resolve status", build(t, newServer(t, status(code))).Resolve(context.Background(), "k", channel.Message{}))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = build(t, newServer(t, nil)).Send(ctx, msg(), active)
	check("cancelled", err)
	bad := msg()
	bad.Link = "https://x.example/" + fakeKey + "\x7f"
	_, err = build(t, newServer(t, nil)).Send(context.Background(), bad, active)
	check("bad link", err)
	bad = msg()
	bad.Actions = []channel.Action{{Label: "a", URL: "javascript:" + fakeKey}}
	_, err = build(t, newServer(t, nil)).Send(context.Background(), bad, active)
	check("bad action", err)
	_, err = build(t, newServer(t, nil)).Send(context.Background(), msg(), channel.SendSpec{Interruption: "loud"})
	check("bad interruption", err)
}

func TestContextCancellationIsTransient(t *testing.T) {
	c := build(t, newServer(t, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Send(ctx, msg(), active)
	if err == nil || channel.KindOf(err) != channel.TransientError || !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled send: err=%v kind=%v", err, channel.KindOf(err))
	}
	err = c.Resolve(ctx, "k", channel.Message{})
	if err == nil || channel.KindOf(err) != channel.TransientError || !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled resolve: err=%v kind=%v", err, channel.KindOf(err))
	}
	dctx, dcancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer dcancel()
	time.Sleep(time.Millisecond)
	_, err = c.Send(dctx, msg(), active)
	if err == nil || channel.KindOf(err) != channel.TransientError || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("deadline send: err=%v kind=%v", err, channel.KindOf(err))
	}
}

func TestReturnsPromptlyWhenServerHangs(t *testing.T) {
	release := make(chan struct{})
	f := newServer(t, func(http.ResponseWriter, *http.Request) { <-release })
	t.Cleanup(func() { close(release) })
	c := build(t, f)
	calls := map[string]func(context.Context) error{
		"send": func(ctx context.Context) error { _, err := c.Send(ctx, msg(), active); return err },
		"resolve": func(ctx context.Context) error {
			return c.Resolve(ctx, "k", channel.Message{})
		},
	}
	for name, call := range calls {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(50*time.Millisecond, cancel)
		start := time.Now()
		err := call(ctx)
		if d := time.Since(start) - 50*time.Millisecond; d > 200*time.Millisecond {
			t.Errorf("%s took %v after cancel, want within 200ms", name, d)
		}
		if err == nil || channel.KindOf(err) != channel.TransientError {
			t.Errorf("%s: err=%v kind=%v", name, err, channel.KindOf(err))
		}
	}
}
