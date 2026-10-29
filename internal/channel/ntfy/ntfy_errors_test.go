package ntfy

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

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		// a hostile or sloppy server may echo what it was given
		_, _ = io.WriteString(w, `{"error":"no such topic `+fakeTopic+` token `+fakeToken+`"}`)
	}
}

func TestErrorKinds(t *testing.T) {
	cases := map[int]channel.ErrorKind{
		400: channel.PermanentError, 401: channel.PermanentError, 403: channel.PermanentError,
		413: channel.PermanentError, 429: channel.TransientError, 503: channel.TransientError,
		500: channel.TransientError, 408: channel.TransientError, 302: channel.PermanentError,
	}
	for code, want := range cases {
		f := newServer(t, status(code))
		c := build(t, f, map[string]string{"topic": fakeTopic, "token": fakeToken})
		_, err := c.Send(context.Background(), msg(), channel.SendSpec{Interruption: channel.Active})
		if err == nil || channel.KindOf(err) != want {
			t.Errorf("send %d: err=%v kind=%v, want %v", code, err, channel.KindOf(err), want)
		}
		err = c.Cancel(context.Background(), channel.Receipt{ProviderID: "x"})
		if err == nil || channel.KindOf(err) != want {
			t.Errorf("cancel %d: err=%v kind=%v, want %v", code, err, channel.KindOf(err), want)
		}
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	other := newServer(t, nil)
	f := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	})
	c := build(t, f, map[string]string{"topic": fakeTopic, "token": fakeToken})
	_, err := c.Send(context.Background(), msg(), channel.SendSpec{Interruption: channel.Active})
	if err == nil || channel.KindOf(err) != channel.PermanentError {
		t.Errorf("err=%v kind=%v, want permanent", err, channel.KindOf(err))
	}
	if n := len(other.all()); n != 0 {
		t.Errorf("redirect target got %d requests (the token would leak)", n)
	}
}

func TestDialErrorIsTransient(t *testing.T) {
	f := newServer(t, nil)
	c := build(t, f, nil)
	f.Close()
	_, err := c.Send(context.Background(), msg(), channel.SendSpec{Interruption: channel.Active})
	if err == nil || channel.KindOf(err) != channel.TransientError {
		t.Errorf("send err=%v kind=%v", err, channel.KindOf(err))
	}
	err = c.Cancel(context.Background(), channel.Receipt{ProviderID: "x"})
	if err == nil || channel.KindOf(err) != channel.TransientError {
		t.Errorf("cancel err=%v kind=%v", err, channel.KindOf(err))
	}
}

func TestNoSecretInErrors(t *testing.T) {
	secrets := map[string]string{"topic": fakeTopic, "token": fakeToken}
	check := func(label string, err error) {
		t.Helper()
		if err == nil {
			t.Errorf("%s: no error", label)
			return
		}
		for e := error(err); e != nil; e = errors.Unwrap(e) {
			s := e.Error()
			if strings.Contains(s, fakeTopic) || strings.Contains(s, fakeToken) || strings.Contains(s, "FAKE") {
				t.Errorf("%s: secret in error text %q", label, s)
			}
		}
	}
	// dial error
	d := newServer(t, nil)
	dc := build(t, d, secrets)
	d.Close()
	_, err := dc.Send(context.Background(), msg(), channel.SendSpec{Interruption: channel.Active})
	check("dial send", err)
	check("dial cancel", dc.Cancel(context.Background(), channel.Receipt{ProviderID: "x"}))
	// error responses whose body echoes the topic
	for _, code := range []int{400, 401, 429, 503} {
		f := newServer(t, status(code))
		c := build(t, f, secrets)
		_, err := c.Send(context.Background(), msg(), channel.SendSpec{Interruption: channel.Active})
		check("body send", err)
		check("body cancel", c.Cancel(context.Background(), channel.Receipt{ProviderID: "x"}))
	}
	// a send cut short by the context
	f := newServer(t, nil)
	cc := build(t, f, secrets)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = cc.Send(ctx, msg(), channel.SendSpec{Interruption: channel.Active})
	check("cancelled send", err)
	check("cancelled cancel", cc.Cancel(ctx, channel.Receipt{ProviderID: "x"}))
}

func TestContextCancellationIsTransient(t *testing.T) {
	f := newServer(t, nil)
	c := build(t, f, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Send(ctx, msg(), channel.SendSpec{Interruption: channel.Active})
	if err == nil || channel.KindOf(err) != channel.TransientError {
		t.Errorf("cancelled send: err=%v kind=%v", err, channel.KindOf(err))
	}
	dctx, dcancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer dcancel()
	time.Sleep(time.Millisecond)
	_, err = c.Send(dctx, msg(), channel.SendSpec{Interruption: channel.Active})
	if err == nil || channel.KindOf(err) != channel.TransientError {
		t.Errorf("deadline send: err=%v kind=%v", err, channel.KindOf(err))
	}
	if err := c.Cancel(ctx, channel.Receipt{ProviderID: "x"}); err == nil || channel.KindOf(err) != channel.TransientError {
		t.Errorf("cancelled Cancel: err=%v kind=%v", err, channel.KindOf(err))
	}
}

func TestSendReturnsPromptlyWhenServerHangs(t *testing.T) {
	release := make(chan struct{})
	f := newServer(t, func(http.ResponseWriter, *http.Request) { <-release })
	t.Cleanup(func() { close(release) })
	c := build(t, f, nil)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	_, err := c.Send(ctx, msg(), channel.SendSpec{Interruption: channel.Active})
	if d := time.Since(start); d > 200*time.Millisecond+50*time.Millisecond {
		t.Errorf("Send took %v after cancel, want within 200ms", d-50*time.Millisecond)
	}
	if err == nil || channel.KindOf(err) != channel.TransientError {
		t.Errorf("err=%v kind=%v", err, channel.KindOf(err))
	}
}
