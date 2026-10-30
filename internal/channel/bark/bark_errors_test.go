package bark

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
		_, _ = io.WriteString(w, `{"code":1,"message":"failed to get device token for `+fakeKey+`"}`)
	}
}

// reply answers 200 with a body.
func reply(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }
}

func TestErrorKinds(t *testing.T) {
	cases := map[int]channel.ErrorKind{
		400: channel.PermanentError, 401: channel.PermanentError, 404: channel.PermanentError,
		413: channel.PermanentError, 429: channel.TransientError, 503: channel.TransientError,
		500: channel.TransientError, 408: channel.TransientError, 302: channel.PermanentError,
	}
	for code, want := range cases {
		_, err := build(t, newServer(t, status(code))).Send(context.Background(), msg(), active)
		if err == nil || channel.KindOf(err) != want {
			t.Errorf("send %d: err=%v kind=%v, want %v", code, err, channel.KindOf(err), want)
		}
	}
}

func TestApplicationErrorIn200(t *testing.T) {
	cases := map[string]channel.ErrorKind{
		`{"code":400,"message":"device key ` + fakeKey + ` not found"}`: channel.PermanentError,
		`{"code":404,"message":"x"}`:                                    channel.PermanentError,
		`{"code":500,"message":"push failed"}`:                          channel.TransientError,
		`{"code":429,"message":"slow down"}`:                            channel.TransientError,
		`<html>captive portal ` + fakeKey + `</html>`:                   channel.TransientError,
		`{"message":"no code"}`:                                         channel.TransientError,
		`{"code":"400"}`:                                                channel.TransientError,
		``:                                                              channel.TransientError,
	}
	for body, want := range cases {
		_, err := build(t, newServer(t, reply(body))).Send(context.Background(), msg(), active)
		if err == nil || channel.KindOf(err) != want {
			t.Errorf("%q: err=%v kind=%v, want %v", body, err, channel.KindOf(err), want)
			continue
		}
		if s := err.Error(); strings.Contains(s, "FAKE") || strings.Contains(s, "not found") || strings.Contains(s, "captive") {
			t.Errorf("%q: error echoes the body: %q", body, s)
		}
	}
	if _, err := build(t, newServer(t, reply(`{"code":200,"message":"success"}`))).Send(context.Background(), msg(), active); err != nil {
		t.Errorf("code 200: %v", err)
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	other := newServer(t, nil)
	f := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/push", http.StatusTemporaryRedirect)
	})
	_, err := build(t, f).Send(context.Background(), msg(), active)
	if err == nil || channel.KindOf(err) != channel.PermanentError {
		t.Errorf("err=%v kind=%v, want permanent", err, channel.KindOf(err))
	}
	if n := len(other.all()); n != 0 {
		t.Errorf("redirect target got %d requests (the device key would leak)", n)
	}
}

func TestDialErrorIsTransient(t *testing.T) {
	f := newServer(t, nil)
	c := build(t, f)
	f.Close()
	_, err := c.Send(context.Background(), msg(), active)
	if err == nil || channel.KindOf(err) != channel.TransientError {
		t.Errorf("err=%v kind=%v", err, channel.KindOf(err))
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
			if s := e.Error(); strings.Contains(s, "FAKE") || strings.Contains(s, "127.0.0.1") {
				t.Errorf("%s: secret or address in error text %q", label, s)
			}
		}
	}
	d := newServer(t, nil)
	dc := build(t, d)
	d.Close()
	_, err := dc.Send(context.Background(), msg(), active)
	check("dial", err)
	for _, code := range []int{400, 401, 429, 503} {
		_, err := build(t, newServer(t, status(code))).Send(context.Background(), msg(), active)
		check("status", err)
	}
	_, err = build(t, newServer(t, reply(`{"code":400,"message":"`+fakeKey+`"}`))).Send(context.Background(), msg(), active)
	check("envelope", err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = build(t, newServer(t, nil)).Send(ctx, msg(), active)
	check("cancelled", err)
	bad := msg()
	bad.Link = "https://x.example/" + fakeKey + "\x7f"
	_, err = build(t, newServer(t, nil)).Send(context.Background(), bad, active)
	check("bad link", err)
	_, err = build(t, newServer(t, nil)).Send(context.Background(), msg(), channel.SendSpec{Options: map[string]any{"sound": 1}})
	check("bad option", err)
}

func TestContextCancellationIsTransient(t *testing.T) {
	c := build(t, newServer(t, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Send(ctx, msg(), active)
	if err == nil || channel.KindOf(err) != channel.TransientError || !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled send: err=%v kind=%v", err, channel.KindOf(err))
	}
	dctx, dcancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer dcancel()
	time.Sleep(time.Millisecond)
	_, err = c.Send(dctx, msg(), active)
	if err == nil || channel.KindOf(err) != channel.TransientError || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("deadline send: err=%v kind=%v", err, channel.KindOf(err))
	}
}

func TestSendReturnsPromptlyWhenServerHangs(t *testing.T) {
	release := make(chan struct{})
	f := newServer(t, func(http.ResponseWriter, *http.Request) { <-release })
	t.Cleanup(func() { close(release) })
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	_, err := build(t, f).Send(ctx, msg(), active)
	if d := time.Since(start) - 50*time.Millisecond; d > 200*time.Millisecond {
		t.Errorf("Send took %v after cancel, want within 200ms", d)
	}
	if err == nil || channel.KindOf(err) != channel.TransientError {
		t.Errorf("err=%v kind=%v", err, channel.KindOf(err))
	}
}

func TestTruncatedReplyIsTransient(t *testing.T) {
	f := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, `{"code":`)
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler) // closes the connection mid-body
	})
	_, err := build(t, f).Send(context.Background(), msg(), active)
	if err == nil || channel.KindOf(err) != channel.TransientError || strings.Contains(err.Error(), "FAKE") {
		t.Errorf("err=%v kind=%v", err, channel.KindOf(err))
	}
}
