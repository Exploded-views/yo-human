package web_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/web"
)

// freeAddr returns a loopback address whose port was free a moment ago: it listens on port 0 and
// closes the listener. The race between this and Serve binding it is accepted.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// hold listens on a free loopback port for the whole test, so a bind to it fails.
func hold(t *testing.T) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

func say(word string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, word) })
}

type served struct {
	cancel context.CancelFunc
	done   chan error
}

// start runs Serve in the background and returns how to stop it and where its result comes.
func start(t *testing.T, h web.Handlers, c web.ServeConfig) *served {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := &served{cancel: cancel, done: make(chan error, 1)}
	go func() { s.done <- web.Serve(ctx, h, c) }()
	t.Cleanup(cancel)
	return s
}

func (s *served) result(t *testing.T, within time.Duration) error {
	t.Helper()
	select {
	case err := <-s.done:
		return err
	case <-time.After(within):
		t.Fatalf("Serve did not return within %s", within)
		return nil
	}
}

// fetch GETs url and returns the body, retrying while the listener is not up yet.
func fetch(t *testing.T, url string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return string(b)
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s: %v", url, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func refused(t *testing.T, addr string) bool {
	t.Helper()
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(context.Background(), "tcp", addr)
	if err == nil {
		_ = conn.Close()
		return false
	}
	return true
}

func TestServeRefusesNonLoopbackMCP(t *testing.T) {
	approve := hold(t) // the approval port is taken: only a check before binding can produce the error
	h := web.Handlers{MCP: say("mcp"), Approve: say("approve")}
	for _, addr := range []string{"0.0.0.0:0", "[::]:0", ":0", "localhost:0", "[::ffff:127.0.0.1]:0", "[::1%lo0]:0", "192.0.2.1:0",
		"127.0.0.1", "", "[::ffff:127.0.0.1]:8081"} {
		err := web.Serve(context.Background(), h, web.ServeConfig{MCPAddr: addr, ApproveAddr: approve, Grace: time.Second})
		if !errors.Is(err, web.ErrMCPNotLoopback) {
			t.Errorf("MCPAddr %q: %v, want ErrMCPNotLoopback", addr, err)
		}
	}
}

func TestServeRefusesSamePort(t *testing.T) {
	h := web.Handlers{MCP: say("mcp"), Approve: say("approve")}
	for _, c := range []web.ServeConfig{
		{MCPAddr: "127.0.0.1:18081", ApproveAddr: ":18081"},
		{MCPAddr: "127.0.0.1:18081", ApproveAddr: "0.0.0.0:18081"},
		{MCPAddr: "127.0.0.1:18081", ApproveAddr: "[::]:18081"},
		{MCPAddr: "127.0.0.1:18081", ApproveAddr: ":018081"}, // the same number, spelled differently
	} {
		c.Grace = time.Second
		if err := web.Serve(context.Background(), h, c); !errors.Is(err, web.ErrSamePort) {
			t.Errorf("%+v: %v, want ErrSamePort", c, err)
		}
	}
}

// A non-loopback MCP address is refused as that, whatever the ports are.
func TestServeChecksLoopbackBeforePorts(t *testing.T) {
	h := web.Handlers{MCP: say("mcp"), Approve: say("approve")}
	err := web.Serve(context.Background(), h, web.ServeConfig{MCPAddr: "0.0.0.0:18081", ApproveAddr: ":18081", Grace: time.Second})
	if !errors.Is(err, web.ErrMCPNotLoopback) {
		t.Errorf("%v, want ErrMCPNotLoopback", err)
	}
}

func TestServeStopsBothWhenOneFails(t *testing.T) {
	approve := hold(t)
	mcp := freeAddr(t)
	s := start(t, web.Handlers{MCP: say("mcp"), Approve: say("approve")}, web.ServeConfig{MCPAddr: mcp, ApproveAddr: approve, Grace: time.Second})
	err := s.result(t, time.Second)
	if err == nil || !strings.Contains(err.Error(), "approval") || errors.Is(err, web.ErrForcedClose) {
		t.Fatalf("Serve = %v, want a bind error naming the approval listener", err)
	}
	if !refused(t, mcp) {
		t.Error("the MCP listener still accepts after Serve returned")
	}
}

func TestServeNamesTheMCPListenerWhenItFails(t *testing.T) {
	mcp := hold(t)
	s := start(t, web.Handlers{MCP: say("mcp"), Approve: say("approve")}, web.ServeConfig{MCPAddr: mcp, ApproveAddr: freeAddr(t), Grace: time.Second})
	if err := s.result(t, time.Second); err == nil || !strings.Contains(err.Error(), "MCP") {
		t.Fatalf("Serve = %v, want a bind error naming the MCP listener", err)
	}
}

// With no request in flight, a cancelled context drains at once and Serve returns nil; each
// listener serves its own handler until then.
func TestServeRoutesEachListenerAndEndsCleanly(t *testing.T) {
	mcp, approve := freeAddr(t), freeAddr(t)
	s := start(t, web.Handlers{MCP: say("mcp"), Approve: say("approve")}, web.ServeConfig{MCPAddr: mcp, ApproveAddr: approve, Grace: time.Second})
	if got := fetch(t, "http://"+mcp+"/"); got != "mcp" {
		t.Errorf("MCP listener served %q", got)
	}
	if got := fetch(t, "http://"+approve+"/"); got != "approve" {
		t.Errorf("approval listener served %q", got)
	}
	s.cancel()
	if err := s.result(t, time.Second); err != nil {
		t.Errorf("Serve = %v, want nil", err)
	}
	if !refused(t, mcp) || !refused(t, approve) {
		t.Error("a listener still accepts after Serve returned")
	}
}

// A request that outlives Grace is cut: Serve returns ErrForcedClose and the connection closes.
func TestServeGraceThenForcedClose(t *testing.T) {
	mcp, approve := freeAddr(t), freeAddr(t)
	inFlight, released := make(chan struct{}), make(chan struct{})
	block := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(inFlight)
		<-r.Context().Done()
		close(released)
	})
	s := start(t, web.Handlers{MCP: block, Approve: say("approve")}, web.ServeConfig{MCPAddr: mcp, ApproveAddr: approve, Grace: 200 * time.Millisecond})
	fetch(t, "http://"+approve+"/") // the approval listener is up; the MCP one binds with it
	clientErr := blockedGet("http://" + mcp + "/")
	select {
	case <-inFlight:
	case <-time.After(3 * time.Second):
		t.Fatal("the blocking request never reached its handler")
	}
	began := time.Now()
	s.cancel()
	if err := s.result(t, time.Second); !errors.Is(err, web.ErrForcedClose) {
		t.Fatalf("Serve = %v, want ErrForcedClose", err)
	}
	if took := time.Since(began); took < 150*time.Millisecond {
		t.Errorf("Serve returned after %s, before the %s grace", took, 200*time.Millisecond)
	}
	waitFor(t, released, "the blocked request's context never ended")
	select {
	case err := <-clientErr:
		if err == nil {
			t.Error("the blocked request completed normally; its connection should be cut")
		}
	case <-time.After(time.Second):
		t.Error("the blocked request's connection was not closed")
	}
}

func waitFor(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Error(msg)
	}
}

// blockedGet sends a GET in the background and returns where its outcome arrives.
func blockedGet(url string) <-chan error {
	out := make(chan error, 1)
	go func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_, err = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
		}
		out <- err
	}()
	return out
}

// A request that finishes inside Grace is drained, not cut: Serve returns nil.
func TestServeDrainsWithinGrace(t *testing.T) {
	mcp, approve := freeAddr(t), freeAddr(t)
	inFlight := make(chan struct{})
	slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(inFlight)
		time.Sleep(100 * time.Millisecond)
		_, _ = io.WriteString(w, "done")
	})
	s := start(t, web.Handlers{MCP: slow, Approve: say("approve")}, web.ServeConfig{MCPAddr: mcp, ApproveAddr: approve, Grace: 2 * time.Second})
	fetch(t, "http://"+approve+"/")
	body := make(chan string, 1)
	go func() { body <- fetch(t, "http://"+mcp+"/") }()
	<-inFlight
	s.cancel()
	if err := s.result(t, 2*time.Second); err != nil {
		t.Errorf("Serve = %v, want nil: the request finished inside the grace", err)
	}
	if got := <-body; got != "done" {
		t.Errorf("the drained request got %q", got)
	}
}
