// Package transport holds the network helpers every channel implementation shares: the HTTP
// client that never follows a redirect, the fixed-words transport error, and the check of a
// server setting. Only the implementations import it; the internal/channel interface imports no
// network package (constitution IV, single network path).
package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/Exploded-views/yo-human/internal/channel"
)

// TransportError describes an HTTP client failure in fixed words, as a Transient error. The
// *url.Error the client returns embeds the request URL, which can hold a secret, so the raw error
// is never returned or wrapped; only the context's own error is, so callers can test for it.
// provider prefixes the text ("ntfy request timed out").
func TransportError(ctx context.Context, provider string, err error) error {
	var (
		ne   net.Error
		cert *tls.CertificateVerificationError
		op   *net.OpError
	)
	switch {
	case ctx.Err() != nil:
		return channel.Transient(fmt.Errorf("%s request interrupted: %w", provider, ctx.Err()))
	case errors.As(err, &cert):
		return channel.Transient(fmt.Errorf("%s server certificate could not be verified", provider))
	case errors.As(err, &ne) && ne.Timeout():
		return channel.Transient(fmt.Errorf("%s request timed out", provider))
	case errors.As(err, &op):
		return channel.Transient(fmt.Errorf("%s server could not be reached", provider))
	}
	return channel.Transient(fmt.Errorf("%s request failed", provider))
}

// NoRedirectClient returns a copy of base (a default client when nil) that never follows a
// redirect, so a request body or credential is not forwarded to another host. The client sets no
// timeout of its own; the context carries the deadline.
func NoRedirectClient(base *http.Client) *http.Client {
	c := http.Client{}
	if base != nil {
		c = *base
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// ServerURL checks a channel's server setting and returns it without a trailing slash: an https URL
// with a host (a port alone is no host), without credentials, a query (even an empty "?", which
// would turn every path the channel appends into a query) or a fragment.
func ServerURL(s string) (string, bool) {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	return strings.TrimRight(u.String(), "/"), true
}
