//go:build e2e

package e2e

import (
	"encoding/json"
	"encoding/pem"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// rec is one request a fake received.
type rec struct {
	Method, Path string
	Auth         string // the Authorization header, "" when absent
	Body         []byte
}

// obj decodes a JSON request body.
func (r rec) obj(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body, &m); err != nil {
		t.Fatalf("%s %s: body is not a JSON object: %v", r.Method, r.Path, err)
	}
	return m
}

// fake stands in for one provider. It records every request and answers with the status set by
// down (0 means the provider's normal success answer); delete requests have their own status.
type fake struct {
	kind  string
	srv   *httptest.Server
	mu    sync.Mutex
	reqs  []rec
	code  atomic.Int32
	delay atomic.Int64 // nanoseconds each answer waits, to widen race windows
	dcode atomic.Int32
}

func newFake(t *testing.T, kind string) *fake {
	t.Helper()
	f := &fake{kind: kind}
	f.srv = httptest.NewUnstartedServer(http.HandlerFunc(f.serve))
	f.srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	f.srv.StartTLS()
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.reqs = append(f.reqs, rec{r.Method, r.URL.Path, r.Header.Get("Authorization"), body})
	f.mu.Unlock()
	if d := time.Duration(f.delay.Load()); d > 0 {
		time.Sleep(d)
	}
	code := f.code.Load()
	if r.Method == http.MethodDelete {
		code = f.dcode.Load()
	}
	if code != 0 {
		w.WriteHeader(int(code))
		return
	}
	switch f.kind {
	case "bark":
		_, _ = w.Write([]byte(`{"code":200,"message":"success"}`))
	case "pagerduty":
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"success"}`))
	default:
		_, _ = w.Write([]byte(`{"event":"message"}`))
	}
}

func (f *fake) url() string   { return f.srv.URL }
func (f *fake) down(code int) { f.code.Store(int32(code)) }
func (f *fake) up()           { f.code.Store(0) }

// requests is a copy of what the fake has received so far.
func (f *fake) requests() []rec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]rec(nil), f.reqs...)
}

// only returns the fake's single request, failing the test if there is not exactly one.
func (f *fake) only(t *testing.T) rec {
	t.Helper()
	got := f.requests()
	if len(got) != 1 {
		t.Fatalf("%s fake received %d requests, want 1: %v", f.kind, len(got), summarize(got))
	}
	return got[0]
}

// wantCount fails unless the fake has received exactly n requests.
func (f *fake) wantCount(t *testing.T, n int) {
	t.Helper()
	if got := f.requests(); len(got) != n {
		t.Fatalf("%s fake received %d requests, want %d: %v", f.kind, len(got), n, summarize(got))
	}
}

func summarize(rs []rec) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Method+" "+r.Path)
	}
	return out
}

// caPEM is the PEM of the certificates the fakes serve.
func caPEM(fakes ...*fake) []byte {
	var out []byte
	for _, f := range fakes {
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.srv.Certificate().Raw})...)
	}
	return out
}
