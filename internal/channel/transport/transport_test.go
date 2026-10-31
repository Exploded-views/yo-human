package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

const fakeSecret = "SEC-FAKE-1"

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout " + fakeSecret }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestTransportErrorDescribesInFixedWords(t *testing.T) {
	cases := map[string]error{
		"unknown": errors.New("boom " + fakeSecret),
		"timeout": timeoutErr{},
		"cert":    &tls.CertificateVerificationError{Err: errors.New("bad cert " + fakeSecret)},
		"dial":    &net.OpError{Op: "dial", Err: errors.New("refused " + fakeSecret)},
		"url":     &urlErr{"https://h.example/" + fakeSecret},
	}
	for name, cause := range cases {
		err := TransportError(context.Background(), "prov", cause, false)
		if err == nil || channel.KindOf(err) != channel.TransientError {
			t.Errorf("%s: err=%v kind=%v", name, err, channel.KindOf(err))
			continue
		}
		for e := err; e != nil; e = errors.Unwrap(e) {
			if s := e.Error(); strings.Contains(s, "FAKE") || strings.Contains(s, "h.example") || !strings.HasPrefix(s, "prov ") {
				t.Errorf("%s: error text %q leaks the cause or lacks the provider", name, s)
			}
		}
	}
}

// urlErr mimics *url.Error: its text embeds the URL.
type urlErr struct{ url string }

func (e *urlErr) Error() string { return "Post " + e.url + ": connection refused" }

func TestTransportErrorForContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := TransportError(ctx, "prov", errors.New("x "+fakeSecret), false)
	if channel.KindOf(err) != channel.TransientError || !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "FAKE") {
		t.Errorf("cancelled: %v", err)
	}
	dctx, dcancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer dcancel()
	time.Sleep(time.Millisecond)
	if err := TransportError(dctx, "prov", errors.New("x"), false); !errors.Is(err, context.DeadlineExceeded) || channel.KindOf(err) != channel.TransientError {
		t.Errorf("deadline: %v", err)
	}
}

func TestNoRedirectClient(t *testing.T) {
	var hits int
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer target.Close()
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer src.Close()
	orig := &http.Client{Timeout: 5 * time.Second}
	c := NoRedirectClient(orig)
	if c == orig || c.Timeout != 5*time.Second || orig.CheckRedirect != nil {
		t.Errorf("client not a copy: same=%v timeout=%v origCheck=%v", c == orig, c.Timeout, orig.CheckRedirect != nil)
	}
	resp, err := c.Get(src.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect || hits != 0 {
		t.Errorf("status=%d target hits=%d", resp.StatusCode, hits)
	}
	if NoRedirectClient(nil).CheckRedirect == nil {
		t.Error("nil base: redirects not refused")
	}
}

func TestTransportErrorWording(t *testing.T) {
	want := map[string]error{
		"prov request timed out":                        timeoutErr{},
		"prov server could not be reached":              &net.OpError{Op: "dial", Err: errors.New("x")},
		"prov server certificate could not be verified": &tls.CertificateVerificationError{Err: errors.New("x")},
		"prov request failed":                           errors.New("x"),
	}
	for text, cause := range want {
		if got := TransportError(context.Background(), "prov", cause, false).Error(); got != text {
			t.Errorf("got %q, want %q", got, text)
		}
	}
}

// ServerURL is the one check every channel runs on its server setting: an https URL with a host
// (not just a port), without credentials, a query (even an empty one) or a fragment.
func TestServerURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://x.example": "https://x.example", "https://x.example/base/": "https://x.example/base",
		"": "", "http://x.example": "", "https://": "", "https://:443": "", "https://x.example?": "",
		"https://x.example?a=1": "", "https://x.example#f": "", "https://u:p" + "@x.example": "", "://": "", // split in the source so the public-repo scrub stays uniform
	} {
		got, ok := ServerURL(in)
		if got != want || ok != (want != "") {
			t.Errorf("ServerURL(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

// do runs one POST through a Traced request, as a channel does, and returns the mapped error.
func do(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return err
	}
	req, sent := Traced(req)
	resp, err := NoRedirectClient(nil).Do(req)
	if err != nil {
		return TransportError(ctx, "prov", err, sent())
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.ReadAll(resp.Body); err != nil {
		return TransportError(ctx, "prov", err, true)
	}
	return nil
}

func TestTimeoutAfterTheRequestWentOutIsUnconfirmed(t *testing.T) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()
	defer close(done)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err := do(ctx, srv.URL)
	if err == nil || !channel.IsUnconfirmed(err) || channel.KindOf(err) != channel.TransientError {
		t.Errorf("err = %v, unconfirmed = %v; want a transient, unconfirmed error", err, channel.IsUnconfirmed(err))
	}
	if err != nil && strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("error text %q leaks the URL", err)
	}
}

func TestFailureBeforeTheRequestWentOutIsNotUnconfirmed(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	url := "http://" + l.Addr().String()
	_ = l.Close() // nothing listens: connection refused
	if err := do(context.Background(), url); err == nil || channel.IsUnconfirmed(err) {
		t.Errorf("refused: %v unconfirmed=%v; want a plain transient error", err, channel.IsUnconfirmed(err))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := do(ctx, srv.URL); err == nil || channel.IsUnconfirmed(err) {
		t.Errorf("cancelled before Do: %v unconfirmed=%v; want a plain transient error", err, channel.IsUnconfirmed(err))
	}
}

func TestBodyAbortedAfterHeadersIsUnconfirmed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\npartial")
		_ = rw.Flush()
		_ = conn.Close()
	}))
	defer srv.Close()
	if err := do(context.Background(), srv.URL); err == nil || !channel.IsUnconfirmed(err) {
		t.Errorf("err = %v unconfirmed=%v; want an unconfirmed error", err, channel.IsUnconfirmed(err))
	}
}

func TestUnconfirmedUsesTheFixedWords(t *testing.T) {
	err := TransportError(context.Background(), "prov", timeoutErr{}, true)
	if !channel.IsUnconfirmed(err) || err.Error() != "prov request timed out" {
		t.Errorf("err = %q unconfirmed=%v", err, channel.IsUnconfirmed(err))
	}
}

// Go may retry a request on a fresh connection, so each attempt starts with nothing written.
func TestTracedForgetsAWriteWhenAFreshConnectionIsTaken(t *testing.T) {
	req, sent := Traced(httptest.NewRequest(http.MethodPost, "http://127.0.0.1/", nil))
	tr := httptrace.ContextClientTrace(req.Context())
	tr.WroteRequest(httptrace.WroteRequestInfo{})
	if !sent() {
		t.Fatal("a fully written request was not recorded")
	}
	if tr.GetConn == nil {
		t.Fatal("no GetConn hook: a retry on a fresh connection keeps the earlier attempt's flag")
	}
	tr.GetConn("127.0.0.1:80")
	if sent() {
		t.Error("sent() still true after a fresh connection was taken")
	}
	tr.WroteRequest(httptrace.WroteRequestInfo{Err: errors.New("broken pipe")})
	if sent() {
		t.Error("a failed write was recorded as written")
	}
}
