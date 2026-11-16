package web_test

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"sort"
	"strings"
	"testing"
)

// raw sends the bytes as they are over a TCP connection to a real server, so a request line the
// HTTP client would not write (an absolute-form URI, HTTP/1.0 with no Host) reaches the listener.
func (h *harness) raw(handler http.Handler, request string) (status int, body string) {
	h.t.Helper()
	srv := httptest.NewServer(handler)
	h.t.Cleanup(srv.Close)
	conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", srv.Listener.Addr().String())
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := io.WriteString(conn, request); err != nil {
		h.t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func get(path, host string, hdr ...string) req {
	r := req{method: "GET", path: path, host: host, hdr: map[string]string{}}
	for i := 0; i+1 < len(hdr); i += 2 {
		r.hdr[hdr[i]] = hdr[i+1]
	}
	return r
}

func TestApproveListenerServesOnlyApprovalHost(t *testing.T) {
	h := newHarness(t)
	for _, c := range []struct {
		name string
		r    req
		want int
	}{
		{"the MCP host, POST /mcp", req{method: "POST", path: "/mcp", host: mcpHost}, 404},
		{"the MCP host, oauth callback", get("/oauth/github/callback", mcpHost), 404},
		{"the MCP host, resource metadata", get("/.well-known/oauth-protected-resource/mcp", mcpHost), 404},
		{"the MCP host, an approval path", get("/approve/x", mcpHost), 404},
		{"the approval host, POST /mcp", req{method: "POST", path: "/mcp", host: approveHost}, 404},
		{"the approval host, oauth callback", get("/oauth/github/callback", approveHost), 404},
		{"an unknown host, /approve/x", get("/approve/x", "other.example.net"), 404},
		{"the approval host, an unrouted path", get("/", approveHost), 404},
		{"the approval host, /approve/x", get("/approve/x", approveHost), 200},
		{"the approval host, /passkey/enrol", get("/passkey/enrol", approveHost), 200},
		{"the approval host, /healthz", get("/healthz", approveHost), 200},
		{"any host, /healthz", get("/healthz", "other.example.net"), 200},
	} {
		if resp, _ := h.do(h.h.Approve, c.r); resp.StatusCode != c.want {
			t.Errorf("%s: %d, want %d", c.name, resp.StatusCode, c.want)
		}
	}
	if h.extraHits != 0 {
		t.Error("the extra (OAuth) handler was reached through the Approve listener")
	}
}

func TestMCPListenerRefusesApprovalRoutes(t *testing.T) {
	h := newHarness(t)
	for _, host := range []string{mcpHost, approveHost, "other.example.net"} {
		for _, p := range []string{"/approve/x", "/approve", "/approve/static/app.js", "/approvexyz", "/passkey/enrol", "/passkey/", "/passkey/static/app.js"} {
			if resp, _ := h.do(h.h.MCP, get(p, host)); resp.StatusCode != 404 {
				t.Errorf("host %s, %s: %d, want 404", host, p, resp.StatusCode)
			}
		}
	}
	if resp, _ := h.do(h.h.MCP, req{method: "POST", path: "/mcp", host: approveHost, hdr: map[string]string{"Authorization": "Bearer " + h.node}}); resp.StatusCode != 404 {
		t.Errorf("POST /mcp under the approval host: %d, want 404", resp.StatusCode)
	}
	if resp, _ := h.do(h.h.MCP, mcpPost("")); resp.StatusCode != 401 {
		t.Errorf("POST /mcp under the MCP host: %d, want 401 from the bearer check", resp.StatusCode)
	}
	if h.extraHits != 0 {
		t.Errorf("the extra handler served an approval path or the wrong host (%d hits)", h.extraHits)
	}
	for _, p := range []string{"/oauth/github/callback", "/.well-known/oauth-protected-resource/mcp"} {
		if resp, body := h.do(h.h.MCP, get(p, mcpHost)); resp.StatusCode != 200 || body != "extra-route" {
			t.Errorf("%s under the MCP host: %d %q, want the extra handler", p, resp.StatusCode, body)
		}
	}
}

func TestHostNormalisation(t *testing.T) {
	h := newHarness(t)
	if resp, _ := h.do(h.h.MCP, mcpPost("")); resp.StatusCode != 401 {
		t.Fatalf("the plain host: %d, want 401", resp.StatusCode)
	}
	for _, host := range []string{"BUS.Example.COM", "bus.example.com:443", "bus.example.com.", "bus.example.com:80"} {
		r := mcpPost("")
		r.host = host
		if resp, _ := h.do(h.h.MCP, r); resp.StatusCode != 401 {
			t.Errorf("MCP, Host %q: %d, want it served (401 from the bearer check)", host, resp.StatusCode)
		}
	}
	for _, host := range []string{"APPROVE.example.com", "approve.example.com:443", "approve.example.com.", "approve.example.com:80"} {
		if resp, body := h.do(h.h.Approve, get("/approve/x", host)); resp.StatusCode != 200 || body != "approve-page" {
			t.Errorf("Approve, Host %q: %d %q, want it served", host, resp.StatusCode, body)
		}
	}
}

// TestHostNormalisationIgnoresForwardedHeaders: only r.Host counts. A forwarded-host header naming
// this listener's host does not make another Host match, and another port never matches.
func TestHostNormalisationIgnoresForwardedHeaders(t *testing.T) {
	h := newHarness(t)
	for _, f := range []string{"X-Forwarded-Host", "Forwarded", "Fly-Forwarded-Host"} {
		val := func(host string) string {
			if f == "Forwarded" {
				return "host=" + host
			}
			return host
		}
		r := mcpPost("")
		r.host, r.hdr[f] = approveHost, val(mcpHost)
		if resp, _ := h.do(h.h.MCP, r); resp.StatusCode != 404 {
			t.Errorf("MCP, Host %s with %s naming the MCP host: %d, want 404", approveHost, f, resp.StatusCode)
		}
		w := get("/approve/x", mcpHost, f, val(approveHost))
		if resp, _ := h.do(h.h.Approve, w); resp.StatusCode != 404 {
			t.Errorf("Approve, Host %s with %s naming the approval host: %d, want 404", mcpHost, f, resp.StatusCode)
		}
	}
	for _, host := range []string{"bus.example.com:8443", "bus.example.com:81"} {
		r := mcpPost("")
		r.host = host
		if resp, _ := h.do(h.h.MCP, r); resp.StatusCode != 404 {
			t.Errorf("MCP, Host %q: %d, want 404", host, resp.StatusCode)
		}
	}
	if resp, _ := h.do(h.h.Approve, get("/approve/x", "approve.example.com:8443")); resp.StatusCode != 404 {
		t.Errorf("Approve, the right host on another port: %d, want 404", resp.StatusCode)
	}
}

func TestHostNormalisationRawRequests(t *testing.T) {
	h := newHarness(t)
	for _, c := range []struct {
		name    string
		handler http.Handler
		request string
	}{
		{"MCP, HTTP/1.0 with no Host", h.h.MCP, "POST /mcp HTTP/1.0\r\nContent-Length: 0\r\n\r\n"},
		{"Approve, HTTP/1.0 with no Host", h.h.Approve, "GET /approve/x HTTP/1.0\r\n\r\n"},
		{"MCP, an absolute-form URI naming the approval host", h.h.MCP,
			"POST http://approve.example.com/mcp HTTP/1.1\r\nHost: bus.example.com\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"},
		{"Approve, an absolute-form URI naming the MCP host", h.h.Approve,
			"GET http://bus.example.com/approve/x HTTP/1.1\r\nHost: approve.example.com\r\nConnection: close\r\n\r\n"},
	} {
		if status, _ := h.raw(c.handler, c.request); status != 404 {
			t.Errorf("%s: %d, want 404", c.name, status)
		}
	}
}

// TestAbsoluteFormURIHostIsTheHost: Go reads the host of an absolute-form request URI into r.Host
// (RFC 9112 3.2.2), so it is the one host value there is, whatever the Host header says. Each
// listener serves only its own host, by that value.
func TestAbsoluteFormURIHostIsTheHost(t *testing.T) {
	h := newHarness(t)
	status, _ := h.raw(h.h.MCP, "POST http://bus.example.com/mcp HTTP/1.1\r\nHost: approve.example.com\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
	if status != 401 {
		t.Errorf("MCP, URI naming the MCP host, Host header naming the approval host: %d, want 401 (served)", status)
	}
	status, body := h.raw(h.h.Approve, "GET http://approve.example.com/approve/x HTTP/1.1\r\nHost: bus.example.com\r\nConnection: close\r\n\r\n")
	if status != 200 || body != "approve-page" {
		t.Errorf("Approve, URI naming the approval host, Host header naming the MCP host: %d %q, want it served", status, body)
	}
}

// TestNonCanonicalPathsAre404: a path path.Clean would change never reaches routing, so no later
// handler has to clean one, on either listener.
func TestNonCanonicalPathsAre404(t *testing.T) {
	h := newHarness(t)
	for _, p := range []string{"/approve/../mcp", "/approve/./x", "/approve//x", "/passkey/../approve/x", "/a/../mcp", "//mcp", "/./mcp", "/mcp/", "/healthz/", "/approve/x/"} {
		for name, c := range map[string]struct {
			handler http.Handler
			host    string
		}{"MCP": {h.h.MCP, mcpHost}, "Approve": {h.h.Approve, approveHost}} {
			status, body := h.raw(c.handler, "GET "+p+" HTTP/1.1\r\nHost: "+c.host+"\r\nConnection: close\r\n\r\n")
			if status != 404 || body != "404 page not found\n" {
				t.Errorf("%s %s: %d %q, want the shared 404", name, p, status, body)
			}
		}
	}
	if h.extraHits != 0 {
		t.Error("extra served a non-canonical path")
	}
}

// TestHostNormalisationHTTP2: an HTTP/2 request's :authority is its host, and a listener that
// reads r.Host sees it. :authority naming the other host is 404.
func TestHostNormalisationHTTP2(t *testing.T) {
	h := newHarness(t)
	for _, c := range []struct {
		name    string
		handler http.Handler
		path    string
		host    string
		want    int
	}{
		{"MCP, :authority = the approval host", h.h.MCP, "/mcp", approveHost, 404},
		{"MCP, :authority = the MCP host", h.h.MCP, "/mcp", mcpHost, 401},
		{"Approve, :authority = the MCP host", h.h.Approve, "/approve/x", mcpHost, 404},
		{"Approve, :authority = the approval host", h.h.Approve, "/approve/x", approveHost, 200},
	} {
		srv := httptest.NewUnstartedServer(c.handler)
		srv.EnableHTTP2 = true
		srv.StartTLS()
		r, err := http.NewRequestWithContext(context.Background(), "GET", srv.URL+c.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Host = c.host
		client := srv.Client()
		client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // a test server's own certificate
		resp, err := client.Do(r)
		if err != nil {
			srv.Close()
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		srv.Close()
		if resp.ProtoMajor != 2 || resp.StatusCode != c.want {
			t.Errorf("%s: HTTP/%d status %d, want HTTP/2 status %d", c.name, resp.ProtoMajor, resp.StatusCode, c.want)
		}
	}
}

// shape is a response as a prober sees it: status, header names and values (Date excluded) and body.
func shape(resp *result, body string) string {
	var hs []string
	for k, v := range resp.Header {
		if textproto.CanonicalMIMEHeaderKey(k) != "Date" {
			hs = append(hs, k+"="+strings.Join(v, ","))
		}
	}
	sort.Strings(hs)
	return resp.Status + "|" + strings.Join(hs, ";") + "|" + body
}

func TestNotFoundIdentical(t *testing.T) {
	h := newHarness(t)
	var shapes []string
	for _, c := range []struct {
		handler http.Handler
		r       req
	}{
		{h.h.MCP, get("/mcp", "other.example.net")},
		{h.h.MCP, get("/nothing", mcpHost)},
		{h.h.MCP, get("/approve/x", mcpHost)},
		{h.h.Approve, get("/approve/x", "other.example.net")},
		{h.h.Approve, get("/nothing", approveHost)},
		{h.h.Approve, get("/mcp", approveHost)},
	} {
		resp, body := h.do(c.handler, c.r)
		if resp.StatusCode != 404 {
			t.Fatalf("%s: %d, want 404", c.r.path, resp.StatusCode)
		}
		shapes = append(shapes, shape(resp, body))
	}
	for i, s := range shapes {
		if s != shapes[0] {
			t.Errorf("404 %d differs from the first: %q vs %q", i, s, shapes[0])
		}
	}
	rm, bm := h.do(h.h.MCP, get("/healthz", "other.example.net"))
	ra, ba := h.do(h.h.Approve, get("/healthz", mcpHost))
	if shape(rm, bm) != shape(ra, ba) || bm != "ok" || rm.StatusCode != 200 {
		t.Errorf("/healthz differs between listeners or is not ok: %q vs %q", shape(rm, bm), shape(ra, ba))
	}
}
