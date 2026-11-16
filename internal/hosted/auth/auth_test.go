package auth_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/auth"
	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

var t0 = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)

// fakeStore is an in-memory TokenStore keyed by hash.
type fakeStore struct {
	nodes    map[string]bus.Node
	starters map[string]bus.Starter
	desks    map[string]bus.Desk
	access   map[string]bus.Access
	touches  []string
	err      error
}

func newStore() *fakeStore {
	return &fakeStore{nodes: map[string]bus.Node{}, starters: map[string]bus.Starter{}, desks: map[string]bus.Desk{}, access: map[string]bus.Access{}}
}

func (f *fakeStore) NodeByHash(h string) (bus.Node, bool, error) {
	n, ok := f.nodes[h]
	return n, ok, f.err
}

func (f *fakeStore) AccessByHash(h string) (bus.Access, bool, error) {
	a, ok := f.access[h]
	return a, ok, f.err
}

func (f *fakeStore) DeskByHash(h string) (bus.Desk, bool, error) {
	d, ok := f.desks[h]
	return d, ok, f.err
}

func (f *fakeStore) StarterByHash(h string) (bus.Starter, bool, error) {
	s, ok := f.starters[h]
	return s, ok, f.err
}

func (f *fakeStore) TouchNode(name string, at time.Time) error {
	f.touches = append(f.touches, name)
	for h, n := range f.nodes {
		if n.Name == name {
			n.LastUsed = at
			f.nodes[h] = n
		}
	}
	return f.err
}

func mint(t *testing.T, prefix string) (token, hash string) {
	t.Helper()
	token, hash, err := auth.Mint(prefix)
	if err != nil {
		t.Fatalf("Mint(%q): %v", prefix, err)
	}
	return token, hash
}

func verifier(s auth.TokenStore, now *time.Time) *auth.Verifier {
	return auth.NewVerifier(s, func() time.Time { return *now }, 15*time.Minute)
}

func TestMintShapeAndHash(t *testing.T) {
	gitleaks := regexp.MustCompile(`yh[narhdx]_[a-z2-7]{52}`)
	shape := regexp.MustCompile(`^yh[nh]_[a-z2-7]{52}$`)
	seen := map[string]bool{}
	for _, p := range []string{"yhn_", "yhh_"} {
		token, hash := mint(t, p)
		if !shape.MatchString(token) || !gitleaks.MatchString(token) {
			t.Fatalf("a %s token has the wrong shape: length %d", p, len(token))
		}
		if hash != auth.Hash(token) || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(hash) {
			t.Fatalf("hash of a %s token is not Hash(token) in hex", p)
		}
		if seen[token] {
			t.Fatal("two mints gave the same token")
		}
		seen[token] = true
	}
}

func TestMintAllPrefixes(t *testing.T) {
	gitleaks := regexp.MustCompile(`yh[narhdx]_[a-z2-7]{52}`)
	for _, p := range []string{"yhn_", "yha_", "yhr_", "yhd_", "yhh_", "yhx_"} {
		if token, _ := mint(t, p); !gitleaks.MatchString(token) || len(token) != 56 || !strings.HasPrefix(token, p) {
			t.Fatalf("a %s token is %d characters, want 56 and the gitleaks shape", p, len(token))
		}
	}
}

func TestMintRefusesUnknownPrefix(t *testing.T) {
	for _, bad := range []string{"", "yh_", "yhz_", "YHN_", "tok_"} {
		if _, _, err := auth.Mint(bad); err == nil {
			t.Fatalf("Mint(%q) succeeded", bad)
		}
	}
}

func TestHashKnownVector(t *testing.T) {
	// SHA-256 of "abc" in hex: the plan says the hash is SHA-256, hex.
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := auth.Hash("abc"); got != want {
		t.Fatalf("Hash(abc) = %s", got)
	}
}

func TestVerifyNode(t *testing.T) {
	s, now := newStore(), t0
	token, h := mint(t, "yhn_")
	s.nodes[h] = bus.Node{Name: "ci-1", Swarm: "crew", Created: t0}
	p, exp, err := verifier(s, &now).Verify(context.Background(), token)
	if err != nil || p != (bus.Principal{Kind: bus.PrincipalNode, Name: "ci-1", Swarm: "crew"}) || !exp.IsZero() {
		t.Fatalf("p=%+v exp=%v err=%v", p, exp, err)
	}
}

func TestVerifyTouchesNodeAtMostOncePerMinute(t *testing.T) {
	s, now := newStore(), t0
	token, h := mint(t, "yhn_")
	s.nodes[h] = bus.Node{Name: "ci-1", Swarm: "crew", Created: t0}
	v := verifier(s, &now)
	for _, at := range []time.Duration{0, 10 * time.Second} {
		now = t0.Add(at)
		if _, _, err := v.Verify(context.Background(), token); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.touches) != 1 {
		t.Fatalf("touches within a minute = %d, want 1", len(s.touches))
	}
	// A recorded LastUsed inside the minute suppresses the touch; one older than it does not.
	s.nodes[h] = bus.Node{Name: "ci-1", Swarm: "crew", LastUsed: t0}
	now = t0.Add(30 * time.Second)
	_, _, _ = v.Verify(context.Background(), token)
	now = t0.Add(61 * time.Second)
	_, _, _ = v.Verify(context.Background(), token)
	if len(s.touches) != 2 {
		t.Fatalf("touches = %d, want 2", len(s.touches))
	}
}

func TestVerifyTouchFailureDoesNotFailAuth(t *testing.T) {
	s, now := newStore(), t0
	token, h := mint(t, "yhn_")
	s.nodes[h] = bus.Node{Name: "ci-1", Swarm: "crew"}
	v := auth.NewVerifier(touchFails{s}, func() time.Time { return now }, time.Minute)
	if p, _, err := v.Verify(context.Background(), token); err != nil || p.Name != "ci-1" {
		t.Fatalf("p=%+v err=%v", p, err)
	}
}

type touchFails struct{ *fakeStore }

func (touchFails) TouchNode(string, time.Time) error { return errors.New("database is locked") }

func TestDeskTokenTTLAndScope(t *testing.T) {
	s, now := newStore(), t0
	desk, dh := mint(t, "yhh_")
	node, nh := mint(t, "yhn_")
	s.desks[dh] = bus.Desk{Created: t0}
	s.nodes[nh] = bus.Node{Name: "ci-1", Swarm: "crew"}
	v := verifier(s, &now)

	now = t0.Add(15*time.Minute - time.Nanosecond)
	p, exp, err := v.Verify(context.Background(), desk)
	if err != nil || p != (bus.Principal{Kind: bus.PrincipalDesk, Name: "desk"}) || !exp.Equal(t0.Add(15*time.Minute)) {
		t.Fatalf("before the TTL: p=%+v exp=%v err=%v", p, exp, err)
	}
	now = t0.Add(15 * time.Minute)
	if _, _, err := v.Verify(context.Background(), desk); !errors.Is(err, auth.ErrInvalid) {
		t.Fatalf("at the TTL: err=%v, want ErrInvalid", err)
	}

	// A node token never verifies to desk or duet, and the kind comes from the prefix: the same
	// hash under another prefix's store is not a match.
	now = t0
	if p, _, err := v.Verify(context.Background(), node); err != nil || p.Kind != bus.PrincipalNode {
		t.Fatalf("node: p=%+v err=%v", p, err)
	}
	for _, c := range []struct{ name, token, hash string }{
		{"a yhn_ hash in the desk store", node, nh},
		{"a yhh_ hash in the node store", desk, dh},
	} {
		other := newStore()
		other.desks[nh], other.nodes[dh] = bus.Desk{Created: t0}, bus.Node{Name: "x", Swarm: "crew"}
		if _, _, err := verifier(other, &now).Verify(context.Background(), c.token); !errors.Is(err, auth.ErrInvalid) {
			t.Fatalf("%s: err=%v, want ErrInvalid", c.name, err)
		}
	}
}

func TestDeskStarterVerifiesToDeskStartOnly(t *testing.T) {
	s, now := newStore(), t0
	token, h := mint(t, "yhd_")
	s.starters[h] = bus.Starter{Name: "mac", Created: t0}
	p, exp, err := verifier(s, &now).Verify(context.Background(), token)
	want := bus.Principal{Kind: bus.PrincipalDeskStart, Name: "mac"}
	if err != nil || p != want || p.Swarm != "" || !exp.IsZero() {
		t.Fatalf("p=%+v exp=%v err=%v", p, exp, err)
	}
	s.starters[h] = bus.Starter{Name: "mac", Revoked: t0}
	if _, _, err := verifier(s, &now).Verify(context.Background(), token); !errors.Is(err, auth.ErrInvalid) {
		t.Fatalf("revoked starter: err=%v", err)
	}
}

func TestVerifyAccess(t *testing.T) {
	s, now := newStore(), t0
	token, h := mint(t, "yha_")
	s.access[h] = bus.Access{Expires: t0.Add(time.Hour)}
	v := verifier(s, &now)
	p, exp, err := v.Verify(context.Background(), token)
	if err != nil || p != (bus.Principal{Kind: bus.PrincipalDuet, Name: "duet"}) || !exp.Equal(t0.Add(time.Hour)) {
		t.Fatalf("p=%+v exp=%v err=%v", p, exp, err)
	}
	now = t0.Add(time.Hour)
	if _, _, err := v.Verify(context.Background(), token); !errors.Is(err, auth.ErrInvalid) {
		t.Fatalf("expired: err=%v", err)
	}
	now = t0
	s.access[h] = bus.Access{Expires: t0.Add(time.Hour), Revoked: t0}
	if _, _, err := v.Verify(context.Background(), token); !errors.Is(err, auth.ErrInvalid) {
		t.Fatalf("revoked: err=%v", err)
	}
}

func TestVerifyRevokedAndUnknownLookAlike(t *testing.T) {
	s, now := newStore(), t0
	revoked, rh := mint(t, "yhn_")
	s.nodes[rh] = bus.Node{Name: "ci-1", Swarm: "crew", Revoked: t0}
	unknown, _ := mint(t, "yhn_")
	rdesk, dh := mint(t, "yhh_")
	s.desks[dh] = bus.Desk{Created: t0, Revoked: t0}
	cases := map[string]string{
		"revoked node": revoked, "unknown node": unknown, "revoked desk": rdesk,
		"empty": "", "no prefix": strings.Repeat("a", 52), "short": "yhn_abc",
		"long": unknown + "a", "upper": strings.ToUpper(unknown), "refresh": "yhr_" + strings.Repeat("a", 52),
		"executor": "yhx_" + strings.Repeat("a", 52), "padded": unknown[:55] + "=", "space": " " + unknown,
		"digit 1": unknown[:4] + strings.Repeat("1", 52),
	}
	var first error
	for name, tok := range cases {
		_, _, err := verifier(s, &now).Verify(context.Background(), tok)
		if !errors.Is(err, auth.ErrInvalid) || err.Error() != auth.ErrInvalid.Error() {
			t.Fatalf("%s: err=%v, want the ErrInvalid value itself", name, err)
		}
		if first == nil {
			first = err
		} else if err.Error() != first.Error() {
			t.Fatalf("%s: message %q differs from %q", name, err, first)
		}
	}
}

func TestVerifyStoreErrorIsNotInvalidAndLeaksNothing(t *testing.T) {
	s, now := newStore(), t0
	s.err = errors.New("disk I/O error")
	token, _ := mint(t, "yhn_")
	for _, p := range []string{"yhn_", "yha_", "yhh_", "yhd_"} {
		tok := p + token[4:]
		_, _, err := verifier(s, &now).Verify(context.Background(), tok)
		if err == nil || errors.Is(err, auth.ErrInvalid) || strings.Contains(err.Error(), token[4:]) {
			t.Fatalf("%s: err=%v, want a store error that holds no token", p, err)
		}
	}
}

// A refresh or executor token is no bearer credential for Verify, even if a live row exists
// under its hash: the store is chosen by the prefix, and these two have none.
func TestRefreshAndExecutorTokensNeverVerify(t *testing.T) {
	for _, p := range []string{"yhr_", "yhx_"} {
		s, now := newStore(), t0
		token, h := mint(t, p)
		s.nodes[h] = bus.Node{Name: "ci-1", Swarm: "crew"}
		s.starters[h] = bus.Starter{Name: "mac"}
		s.desks[h] = bus.Desk{Created: t0}
		s.access[h] = bus.Access{Expires: t0.Add(time.Hour)}
		if _, _, err := verifier(s, &now).Verify(context.Background(), token); !errors.Is(err, auth.ErrInvalid) {
			t.Fatalf("%s: err=%v, want ErrInvalid", p, err)
		}
	}
}
