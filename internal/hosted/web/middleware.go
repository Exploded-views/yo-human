package web

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/Exploded-views/yo-human/internal/hosted/auth"
	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxBody is the request body limit: 64 KiB.
const maxBody = 64 << 10

// principalKey is where the verified principal rides in the SDK's TokenInfo.Extra.
const principalKey = "principal"

// mcpPipeline is POST /mcp, the middleware outermost first: the body limit, the Origin allow-list,
// the bearer token (a desk starter is refused), the per-request deadline, then the MCP handler.
// The host check has already run in serveMCP, before any of these.
func (s *service) mcpPipeline(v *auth.Verifier) http.Handler {
	var h http.Handler = mcp.NewStreamableHTTPHandler(s.getServer, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true,
		// The SDK's DNS-rebinding guard refuses a non-local Host on a loopback address, which is
		// every request here: the MCP listener is loopback and the tunnel forwards the public Host.
		// serveMCP's own host check is the stricter one.
		DisableLocalhostProtection: true,
	})
	h = s.deadline(h)
	h = refuseDeskStarter(h)
	h = mcpauth.RequireBearerToken(verifier(v), &mcpauth.RequireBearerTokenOptions{
		ResourceMetadataURL: s.cfg.PublicURL + "/.well-known/oauth-protected-resource/mcp",
		// Node tokens do not expire; Verify has already refused an expired Duet token.
		AllowMissingExpiration: true,
	})(h)
	h = s.originAllowed(h)
	return bodyLimit(h)
}

// bodyLimit refuses a body over 64 KiB: by its declared length at once, and by the reader's limit
// when it is streamed.
func bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxBody {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		next.ServeHTTP(w, r)
	})
}

// originAllowed: an Origin header, when present, must be exactly one of YH_ORIGIN_ALLOW.
func (s *service) originAllowed(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, o := range r.Header.Values("Origin") {
			if !slices.Contains(s.cfg.OriginAllow, o) {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// verifier adapts auth.Verifier to the SDK's TokenVerifier. A revoked, expired, unknown or
// malformed token is the one ErrInvalidToken (401); any other failure is a 500 that names nothing.
func verifier(v *auth.Verifier) mcpauth.TokenVerifier {
	return func(ctx context.Context, token string, _ *http.Request) (*mcpauth.TokenInfo, error) {
		p, exp, err := v.Verify(ctx, token)
		switch {
		case errors.Is(err, auth.ErrInvalid):
			return nil, mcpauth.ErrInvalidToken
		case err != nil:
			return nil, errors.New("token verification failed")
		}
		return &mcpauth.TokenInfo{Expiration: exp, UserID: p.Name, Extra: map[string]any{principalKey: p}}, nil
	}
}

// principalOf is the principal the bearer middleware stored for this request, or the zero
// Principal (no kind, which no tool set serves).
func principalOf(info *mcpauth.TokenInfo) bus.Principal {
	if info == nil {
		return bus.Principal{}
	}
	p, _ := info.Extra[principalKey].(bus.Principal)
	return p
}

// refuseDeskStarter: a desk starter (yhd_) verifies but has no tools; /mcp answers it 403.
func refuseDeskStarter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if principalOf(mcpauth.TokenInfoFromContext(r.Context())).Kind == bus.PrincipalDeskStart {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// deadline bounds the whole request at the tool deadline plus the longest wait a call may ask for;
// each tool call is also cut off at its own, exact deadline (callTool).
func (s *service) deadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), s.cfg.ToolDeadline+bus.MaxWait)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
