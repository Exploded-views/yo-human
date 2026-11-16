package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	nodeP = bus.Principal{Kind: bus.PrincipalNode, Name: "ci-1", Swarm: "crew"}
	duetP = bus.Principal{Kind: bus.PrincipalDuet, Name: "duet"}
	ctx   = context.Background()
)

// bearerHost is a RoundTripper that sends the MCP host and a bearer token, so a client talking to
// an httptest server on a loopback address looks like one reaching the public host.
type bearerHost struct{ token string }

func (b bearerHost) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.Host = mcpHost
	if b.token != "" {
		r2.Header.Set("Authorization", "Bearer "+b.token)
	}
	return http.DefaultTransport.RoundTrip(r2)
}

// session connects an MCP client with the token to the MCP listener.
func (h *harness) session(token string) *mcp.ClientSession {
	h.t.Helper()
	srv := httptest.NewServer(h.h.MCP)
	h.t.Cleanup(srv.Close)
	tr := &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerHost{token}},
		DisableStandaloneSSE: true, MaxRetries: -1}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, tr, nil)
	if err != nil {
		h.t.Fatalf("connect: %v", err)
	}
	h.t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// call calls a tool; err is a protocol error (an unknown tool), not a tool error.
func call(cs *mcp.ClientSession, name string, args any) (*mcp.CallToolResult, error) {
	return cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
}

// obj is a JSON object as the client decodes it.
type obj = map[string]any

// mustOK calls the tool and returns its structured result, which must be a v 1 success whose
// text content is the same JSON.
func mustOK(t *testing.T, cs *mcp.ClientSession, name string, args any) obj {
	t.Helper()
	res, err := call(cs, name, args)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	m := structured(t, res)
	if res.IsError || m["v"] != float64(1) || m["error"] != nil {
		t.Fatalf("%s: isError=%v result=%v, want a v 1 success", name, res.IsError, m)
	}
	assertTextIsStructured(t, name, res, m)
	return m
}

func structured(t *testing.T, res *mcp.CallToolResult) obj {
	t.Helper()
	m, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content = %T %v, want an object", res.StructuredContent, res.StructuredContent)
	}
	return m
}

func assertTextIsStructured(t *testing.T, name string, res *mcp.CallToolResult, m obj) {
	t.Helper()
	if len(res.Content) != 1 {
		t.Fatalf("%s: %d content blocks, want 1", name, len(res.Content))
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("%s: content is %T, want text", name, res.Content[0])
	}
	var text obj
	if err := json.Unmarshal([]byte(tc.Text), &text); err != nil || len(text) != len(m) {
		t.Errorf("%s: text content %q is not the structured JSON (%v)", name, tc.Text, err)
	}
}

// mustFail calls the tool and returns the v 1 error object, which must carry the code.
func mustFail(t *testing.T, cs *mcp.ClientSession, name string, args any, code string) obj {
	t.Helper()
	res, err := call(cs, name, args)
	if err != nil {
		t.Fatalf("%s: protocol error %v, want a tool error %s", name, err, code)
	}
	m := structured(t, res)
	e, _ := m["error"].(obj)
	if !res.IsError || m["v"] != float64(1) || e == nil || e["code"] != code {
		t.Fatalf("%s: isError=%v result=%v, want a v 1 error with code %s", name, res.IsError, m, code)
	}
	for _, k := range []string{"what", "valid", "fix"} {
		if s, _ := e[k].(string); s == "" {
			t.Errorf("%s: error.%s is empty: %v", name, k, e)
		}
	}
	assertTextIsStructured(t, name, res, m)
	return e
}

func (h *harness) counts() db.Counts {
	h.t.Helper()
	c, err := h.d.Check(ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	return c
}

// toolNames lists the tools a session sees, sorted.
func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	var names []string
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

func (h *harness) seed(p bus.Principal, d bus.Draft) bus.Item {
	h.t.Helper()
	r, err := h.b.Post(ctx, p, d)
	if err != nil {
		h.t.Fatalf("seed post: %v", err)
	}
	return r.Item
}

func question(handle string) bus.Draft {
	return bus.Draft{Kind: bus.KindQuestion, Severity: bus.SeverityNormal, To: "human", Handle: handle, Title: "Which port?",
		Action: bus.ActionNone, Urgency: bus.UrgencyDigest}
}

func mergeApproval(handle string) bus.Draft {
	return bus.Draft{Kind: bus.KindApproval, Severity: bus.SeverityHigh, To: "human", Handle: handle, Title: "Merge PR 7",
		Action: bus.ActionMerge, Target: "o/r#7", Repo: "o/r", Ref: "7", BaseRef: "main",
		PinnedCommit: "0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a", Urgency: bus.UrgencyDigest}
}

func callParams(name string, args any) *mcp.CallToolParams {
	return &mcp.CallToolParams{Name: name, Arguments: args}
}

// eq fails the test unless got and want are deeply equal. Tests compare through it, so a check is
// one call rather than a branch.
func eq(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// has fails the test unless every key of want is in got with an equal value.
func has(t *testing.T, what string, got, want obj) {
	t.Helper()
	for k, v := range want {
		eq(t, what+"."+k, got[k], v)
	}
}

// at walks a path of object keys and list positions (ints) through a decoded result; a miss is nil.
func at(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, _ := v.(obj)
			v = m[k]
		case int:
			l, _ := v.([]any)
			if k >= len(l) {
				return nil
			}
			v = l[k]
		}
	}
	return v
}

// unknownTool fails the test unless calling the tool is a protocol error saying it is unknown.
func unknownTool(t *testing.T, cs *mcp.ClientSession, name string) {
	t.Helper()
	_, err := call(cs, name, obj{})
	eq(t, name+" is an unknown tool", err != nil && strings.Contains(err.Error(), "unknown tool"), true)
}
