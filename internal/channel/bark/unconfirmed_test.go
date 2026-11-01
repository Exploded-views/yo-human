package bark

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

// The real client, against a server that reads the whole request and then stalls past the deadline,
// reports an unconfirmed send: the request went out and no answer came.
func TestStalledAnswerIsUnconfirmed(t *testing.T) {
	done := make(chan struct{})
	f := newServer(t, func(http.ResponseWriter, *http.Request) {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	})
	t.Cleanup(func() { close(done) }) // runs before the server closes, which waits for its handlers
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := build(t, f).Send(ctx, msg(), active)
	if err == nil || !channel.IsUnconfirmed(err) || channel.KindOf(err) != channel.TransientError {
		t.Errorf("err = %v unconfirmed=%v; want a transient, unconfirmed error", err, channel.IsUnconfirmed(err))
	}
}

// A server that is not listening is a failure before the request went out: plain transient.
func TestClosedListenerIsPlainTransient(t *testing.T) {
	f := newServer(t, nil)
	c := build(t, f)
	f.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.Send(ctx, msg(), active)
	if err == nil || channel.IsUnconfirmed(err) || channel.KindOf(err) != channel.TransientError {
		t.Errorf("err = %v unconfirmed=%v; want a plain transient error", err, channel.IsUnconfirmed(err))
	}
}

// cutOff answers with a status line and headers that promise a body, then closes the connection.
func cutOff(code string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		_, _ = rw.WriteString("HTTP/1.1 " + code + "\r\nContent-Length: 100\r\n\r\npartial")
		_ = rw.Flush()
		_ = conn.Close()
	}
}

// A complete status line is an answer. A non-2xx whose body breaks off is classified by its status
// and is never unconfirmed; only a 2xx with a broken body is.
func TestBrokenBodyIsClassifiedByItsStatus(t *testing.T) {
	cases := []struct {
		code        string
		kind        channel.ErrorKind
		unconfirmed bool
	}{
		{"503 Service Unavailable", channel.TransientError, false},
		{"502 Bad Gateway", channel.TransientError, false},
		{"401 Unauthorized", channel.PermanentError, false},
		{"418 I'm a teapot", channel.PermanentError, false},
		{"200 OK", channel.TransientError, true},
	}
	for _, c := range cases {
		_, err := build(t, newServer(t, cutOff(c.code))).Send(context.Background(), msg(), active)
		if err == nil || channel.KindOf(err) != c.kind || channel.IsUnconfirmed(err) != c.unconfirmed {
			t.Errorf("%s: err=%v kind=%v unconfirmed=%v; want kind %v unconfirmed %v",
				c.code, err, channel.KindOf(err), channel.IsUnconfirmed(err), c.kind, c.unconfirmed)
		}
	}
}
