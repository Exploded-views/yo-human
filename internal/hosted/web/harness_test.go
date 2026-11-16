package web_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/auth"
	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
	"github.com/Exploded-views/yo-human/internal/hosted/policy"
	"github.com/Exploded-views/yo-human/internal/hosted/web"
)

const (
	mcpHost     = "bus.example.com"
	approveHost = "approve.example.com"
	initBody    = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
)

// tokenStore is an auth.TokenStore over maps keyed by token hash.
type tokenStore struct {
	nodes    map[string]bus.Node
	access   map[string]bus.Access
	starters map[string]bus.Starter
}

func (s *tokenStore) NodeByHash(h string) (bus.Node, bool, error) {
	n, ok := s.nodes[h]
	return n, ok, nil
}
func (s *tokenStore) AccessByHash(h string) (bus.Access, bool, error) {
	a, ok := s.access[h]
	return a, ok, nil
}
func (s *tokenStore) DeskByHash(string) (bus.Desk, bool, error) { return bus.Desk{}, false, nil }
func (s *tokenStore) StarterByHash(h string) (bus.Starter, bool, error) {
	st, ok := s.starters[h]
	return st, ok, nil
}
func (s *tokenStore) TouchNode(string, time.Time) error { return nil }

// harness is a bus over a real database, its verifier, the handlers and fake credentials.
type harness struct {
	t                          *testing.T
	d                          *db.DB
	b                          *bus.Bus
	h                          web.Handlers
	cfg                        web.Config
	node, other, duet, starter string // tokens: a node of swarm crew, a node of swarm other, Duet, a desk starter
	extraHits                  int
}

func mint(t *testing.T, prefix string) (token, hash string) {
	t.Helper()
	tok, h, err := auth.Mint(prefix)
	if err != nil {
		t.Fatal(err)
	}
	return tok, h
}

func newHarness(t *testing.T, edit ...func(*web.Config)) *harness {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(filepath.Join(dir, "bus.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	clock := time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)
	n := 0
	now := func() time.Time { n++; return clock.Add(time.Duration(n) * time.Second) }
	b := bus.New(d, now, bus.Limits{LinkHosts: []string{"github.com"}, Policy: policy.Policy{V: 1, Rules: []policy.Rule{
		{Action: "merge", Repo: "o/r", Ref: "main", Mode: policy.Passkey},
	}}})
	h := &harness{t: t, d: d, b: b}
	ts := &tokenStore{nodes: map[string]bus.Node{}, access: map[string]bus.Access{}, starters: map[string]bus.Starter{}}
	var hash string
	h.node, hash = mint(t, auth.PrefixNode)
	ts.nodes[hash] = bus.Node{Name: "ci-1", Swarm: "crew"}
	h.other, hash = mint(t, auth.PrefixNode)
	ts.nodes[hash] = bus.Node{Name: "ci-2", Swarm: "other"}
	h.duet, hash = mint(t, auth.PrefixAccess)
	ts.access[hash] = bus.Access{Expires: time.Now().Add(time.Hour)}
	h.starter, hash = mint(t, auth.PrefixStarter)
	ts.starters[hash] = bus.Starter{Name: "laptop"}
	h.cfg = web.Config{PublicURL: "https://" + mcpHost, ApproveOrigin: "https://" + approveHost,
		OriginAllow: []string{"https://claude.ai"}, ToolDeadline: 5 * time.Second,
		Approve: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "approve-page") })}
	for _, f := range edit {
		f(&h.cfg)
	}
	extra := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { h.extraHits++; _, _ = io.WriteString(w, "extra-route") })
	h.h = web.New(b, auth.NewVerifier(ts, time.Now, 15*time.Minute), h.cfg, extra)
	return h
}

// req is one request to a handler served by a real httptest server, with the Host the caller names.
type req struct {
	method, path, host string
	hdr                map[string]string
	body               string
}

// result is a response with its body already read.
type result struct {
	StatusCode, ProtoMajor int
	Status                 string
	Header                 http.Header
}

func (h *harness) do(handler http.Handler, r req) (*result, string) {
	h.t.Helper()
	srv := httptest.NewServer(handler)
	h.t.Cleanup(srv.Close)
	hr, err := http.NewRequestWithContext(context.Background(), r.method, srv.URL+r.path, strings.NewReader(r.body))
	if err != nil {
		h.t.Fatal(err)
	}
	hr.Host = r.host
	for k, v := range r.hdr {
		hr.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(hr)
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return &result{StatusCode: resp.StatusCode, ProtoMajor: resp.ProtoMajor, Status: resp.Status, Header: resp.Header}, string(b)
}

func mcpPost(token string) req {
	hdr := map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/event-stream"}
	if token != "" {
		hdr["Authorization"] = "Bearer " + token
	}
	return req{method: "POST", path: "/mcp", host: mcpHost, hdr: hdr, body: initBody}
}
