package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
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
		err := TransportError(context.Background(), "prov", cause)
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
	err := TransportError(ctx, "prov", errors.New("x "+fakeSecret))
	if channel.KindOf(err) != channel.TransientError || !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "FAKE") {
		t.Errorf("cancelled: %v", err)
	}
	dctx, dcancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer dcancel()
	time.Sleep(time.Millisecond)
	if err := TransportError(dctx, "prov", errors.New("x")); !errors.Is(err, context.DeadlineExceeded) || channel.KindOf(err) != channel.TransientError {
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
		if got := TransportError(context.Background(), "prov", cause).Error(); got != text {
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
