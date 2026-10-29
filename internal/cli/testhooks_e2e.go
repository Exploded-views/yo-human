//go:build e2e

package cli

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"time"
)

// installTestHooks is compiled only into the end-to-end test binary (-tags e2e); a release build
// has the no-op in testhooks_release.go and holds neither variable name.
//
// The first variable moves the clock every command reads (an RFC 3339 instant). The second
// replaces the root pool of the HTTP client the channels use with a PEM file of certificates (the
// system roots are no longer trusted), because the provider fakes are TLS servers with a throwaway
// certificate and Go on macOS ignores SSL_CERT_FILE.
// A bad value panics: only the test harness sets them.
func installTestHooks() {
	if v := os.Getenv("YO_HUMAN_TEST_NOW"); v != "" {
		at, err := time.Parse(time.RFC3339, v)
		if err != nil {
			panic("YO_HUMAN_TEST_NOW is not an RFC 3339 time, for example 2026-10-26T12:00:00Z")
		}
		clockHook = func() time.Time { return at }
	}
	if p := os.Getenv("YO_HUMAN_TEST_CA"); p != "" {
		pem, err := os.ReadFile(p) //nolint:gosec // a test-only hook named by the test harness
		pool := x509.NewCertPool()
		if err != nil || !pool.AppendCertsFromPEM(pem) {
			panic("YO_HUMAN_TEST_CA cannot be read or holds no PEM certificate")
		}
		// Channels build a zero http.Client, which uses http.DefaultTransport; its roots are replaced.
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
}
