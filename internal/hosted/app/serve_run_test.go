package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	testMCPHost = "bus.example.org" // required()'s YH_PUBLIC_URL
	initBody    = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
)

// running is a real serve in the background.
type running struct {
	code         chan int
	out, err     *bytes.Buffer
	mcp, approve string
}

func startServe(t *testing.T, env map[string]string) *running {
	t.Helper()
	r := &running{code: make(chan int, 1), out: &bytes.Buffer{}, err: &bytes.Buffer{}, mcp: env["YH_LISTEN_MCP"], approve: env["YH_LISTEN_APPROVE"]}
	go func() { r.code <- Run([]string{"serve"}, envOf(env), r.out, r.err) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if resp, err := get(r.approve, "/healthz", ""); err == nil && resp == 200 {
			return r
		}
		select {
		case c := <-r.code:
			t.Fatalf("serve exited %d before it was up: %s", c, r.err.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("serve never came up")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// signal sends sig to this process, where serve's signal context is the only handler.
func (r *running) signal(t *testing.T, sig os.Signal) int {
	t.Helper()
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Signal(sig); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-r.code:
		return c
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop after the signal")
		return -1
	}
}

// get sends one GET with the Host given (empty: the address) and returns the status.
func get(addr, path, host string) (int, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+path, nil)
	if err != nil {
		return 0, err
	}
	if host != "" {
		req.Host = host
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

func TestServeShutsDownOnSignal(t *testing.T) {
	for _, sig := range []os.Signal{syscall.SIGTERM, os.Interrupt} {
		env := serveEnv(t)
		r := startServe(t, env)
		if code := r.signal(t, sig); code != ExitOK {
			t.Errorf("%v: exit %d, want 0: %s", sig, code, r.err.String())
		}
		for _, addr := range []string{r.mcp, r.approve} {
			if _, err := get(addr, "/healthz", ""); err == nil {
				t.Errorf("%v: %s still answers after serve exited", sig, addr)
			}
		}
	}
}

// With the defaults the MCP listener is on loopback, and the approval listener answers nothing
// for the MCP host.
func TestServeBindsMCPOnLoopbackOnly(t *testing.T) {
	def, err := LoadConfig(envOf(required()))
	if err != nil {
		t.Fatal(err)
	}
	mustBeLoopback(t, def.ListenMCP)
	env := serveEnv(t)
	mustBeLoopback(t, env["YH_LISTEN_MCP"])
	r := startServe(t, env)
	defer func() {
		if code := r.signal(t, syscall.SIGTERM); code != ExitOK {
			t.Errorf("exit %d", code)
		}
	}()
	if got, _ := get(r.approve, "/mcp", testMCPHost); got != http.StatusNotFound {
		t.Errorf("the approval listener answered %d for the MCP host's /mcp, want 404", got)
	}
	if got, _ := get(r.approve, "/oauth/github/callback", testMCPHost); got != http.StatusNotFound {
		t.Errorf("the approval listener answered %d for an OAuth path, want 404", got)
	}
	if got, err := get(r.mcp, "/healthz", ""); err != nil || got != http.StatusOK {
		t.Errorf("the MCP listener: %d, %v", got, err)
	}
}

// The listeners serve the real handlers over the real database: no token is 401, and a node
// token minted by `token create` opens an MCP session.
func TestServeAuthenticatesAgainstTheDatabase(t *testing.T) {
	env := serveEnv(t)
	code, token, stderr := runApp(env, "token", "create", "--name", "ci-1", "--swarm", "crew")
	if code != ExitOK {
		t.Fatalf("token create: %d %s", code, stderr)
	}
	r := startServe(t, env)
	defer func() { r.signal(t, syscall.SIGTERM) }()
	post := func(bearer string) int {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://"+r.mcp+"/mcp", strings.NewReader(initBody))
		req.Host = testMCPHost
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if got := post(""); got != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", got)
	}
	if got := post(strings.TrimSpace(token)); got != http.StatusOK {
		t.Errorf("a node token: %d, want 200", got)
	}
}

func mustBeLoopback(t *testing.T, addr string) {
	t.Helper()
	ap, err := netip.ParseAddrPort(addr)
	if err != nil || !ap.Addr().IsLoopback() || ap.Addr().Is4In6() {
		t.Fatalf("the MCP address %q is not a literal loopback address (%v)", addr, err)
	}
}
