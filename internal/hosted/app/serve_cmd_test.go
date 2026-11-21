package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/web"
)

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

// serveEnv is the smallest environment serve starts with: a database in a temp dir and two free
// loopback ports.
func serveEnv(t *testing.T, kv ...string) map[string]string {
	t.Helper()
	base := []string{"YH_DB_PATH", filepath.Join(t.TempDir(), "data", "bus.db"),
		"YH_LISTEN_MCP", freeAddr(t), "YH_LISTEN_APPROVE", freeAddr(t)}
	return with(append(base, kv...)...)
}

// fakeServe replaces the listeners for the test and records what serve gave them.
type fakeServe struct {
	calls int
	ctx   context.Context
	cfg   web.ServeConfig
	h     web.Handlers
	ret   error
}

type ctxKey struct{}

func replaceServe(t *testing.T, ret error) *fakeServe {
	t.Helper()
	f := &fakeServe{ret: ret}
	oldServe, oldNotify := serveFn, notifyContext
	serveFn = func(ctx context.Context, h web.Handlers, c web.ServeConfig) error {
		f.calls++
		f.ctx, f.h, f.cfg = ctx, h, c
		return f.ret
	}
	notifyContext = func() (context.Context, context.CancelFunc) {
		return context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "signal context"))
	}
	t.Cleanup(func() { serveFn, notifyContext = oldServe, oldNotify })
	return f
}

func runApp(env map[string]string, args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = Run(args, envOf(env), &out, &errb)
	return code, out.String(), errb.String()
}

func TestServeExitCodes(t *testing.T) {
	for _, c := range []struct {
		name string
		ret  error
		want int
	}{
		{"a clean drain", nil, ExitOK},
		{"a forced close", web.ErrForcedClose, ExitOK},
		{"a wrapped forced close", fmt.Errorf("serve: %w", web.ErrForcedClose), ExitOK},
		{"a listener that would not bind", errors.New("approval listener: address in use"), ExitInternal},
		{"a non-loopback MCP address", web.ErrMCPNotLoopback, ExitConfig},
		{"two listeners on one port", web.ErrSamePort, ExitConfig},
	} {
		f := replaceServe(t, c.ret)
		code, _, stderr := runApp(serveEnv(t), "serve")
		if code != c.want || f.calls != 1 {
			t.Errorf("%s: exit %d after %d Serve calls, want exit %d after 1; stderr %q", c.name, code, f.calls, c.want, stderr)
		}
	}
}

func TestExitCodeValuesOfServe(t *testing.T) {
	if ExitInternal != 70 || ExitConfig != 78 {
		t.Fatalf("ExitInternal=%d ExitConfig=%d, want 70 and 78", ExitInternal, ExitConfig)
	}
}

func TestServeFailureIsNamedOnStderr(t *testing.T) {
	replaceServe(t, errors.New("approval listener: address in use"))
	if _, _, stderr := runApp(serveEnv(t), "serve"); !strings.Contains(stderr, "address in use") {
		t.Errorf("stderr %q does not say why serve stopped", stderr)
	}
}

func TestServePassesItsConfigAndSignalContext(t *testing.T) {
	f := replaceServe(t, nil)
	env := serveEnv(t)
	if code, _, stderr := runApp(env, "serve"); code != ExitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	want := web.ServeConfig{MCPAddr: env["YH_LISTEN_MCP"], ApproveAddr: env["YH_LISTEN_APPROVE"], Grace: 10 * time.Second}
	if f.cfg != want {
		t.Errorf("ServeConfig = %+v, want %+v", f.cfg, want)
	}
	if f.ctx.Value(ctxKey{}) != "signal context" {
		t.Error("serve ran under a context other than the one notifyContext gave")
	}
	if f.h.MCP == nil || f.h.Approve == nil {
		t.Errorf("handlers %+v: both listeners need one", f.h)
	}
	select {
	case <-f.ctx.Done():
	default:
		t.Error("the signal context was not stopped when serve returned")
	}
}

func TestServeBadConfigIsRefusedBeforeAnythingOpens(t *testing.T) {
	for _, c := range []struct{ name, key, val string }{
		{"a missing variable", "YH_PUBLIC_URL", ""},
		{"a non-loopback MCP address", "YH_LISTEN_MCP", "0.0.0.0:8081"},
	} {
		f := replaceServe(t, nil)
		env := serveEnv(t, c.key, c.val)
		code, _, stderr := runApp(env, "serve")
		if code != ExitConfig || f.calls != 0 || !strings.Contains(stderr, c.key) || strings.Contains(stderr, "GITHUB-SECRET-FAKE") {
			t.Errorf("%s: exit %d after %d Serve calls, stderr %q; want exit 78, no call, the variable named, no secret", c.name, code, f.calls, stderr)
		}
		if _, err := os.Stat(env["YH_DB_PATH"]); err == nil {
			t.Errorf("%s: the database was created before the configuration was refused", c.name)
		}
	}
}

func TestServeDatabaseThatWillNotOpenIsExitStore(t *testing.T) {
	f := replaceServe(t, nil)
	loose := filepath.Join(t.TempDir(), "loose")
	if err := os.Mkdir(loose, 0o755); err != nil { //nolint:gosec // a directory that is too open, on purpose
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o755); err != nil { //nolint:gosec // the umask may have narrowed it
		t.Fatal(err)
	}
	code, _, stderr := runApp(serveEnv(t, "YH_DB_PATH", filepath.Join(loose, "bus.db")), "serve")
	if code != ExitStore || f.calls != 0 || stderr == "" {
		t.Errorf("exit %d after %d Serve calls, stderr %q; want exit 74, no call and a reason", code, f.calls, stderr)
	}
}

func TestServeTakesNoArguments(t *testing.T) {
	f := replaceServe(t, nil)
	if code, _, stderr := runApp(serveEnv(t), "serve", "extra"); code != ExitUsage || f.calls != 0 || !strings.Contains(stderr, "serve") {
		t.Errorf("exit %d after %d Serve calls, stderr %q; want 64 and no call", code, f.calls, stderr)
	}
	if code, _, _ := runApp(serveEnv(t), "serve", "-h"); code != ExitOK || f.calls != 0 {
		t.Errorf("serve -h: exit %d after %d Serve calls, want 0 and none", code, f.calls)
	}
}

func TestUsageNamesServeAndSelfcheck(t *testing.T) {
	_, _, stderr := runApp(nil)
	for _, w := range []string{"  serve ", "  selfcheck "} {
		if !strings.Contains(stderr, w) {
			t.Errorf("usage lacks %q:\n%s", w, stderr)
		}
	}
}
