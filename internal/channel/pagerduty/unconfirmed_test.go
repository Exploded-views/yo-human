package pagerduty

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
