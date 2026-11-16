package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/auth"
	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

var tokenRE = regexp.MustCompile(`yh[narhdx]_[a-z2-7]{52}`)

type adminEnv struct {
	t    *testing.T
	path string
}

func newAdmin(t *testing.T) adminEnv {
	t.Helper()
	return adminEnv{t: t, path: filepath.Join(t.TempDir(), "data", "bus.db")}
}

func (a adminEnv) env(k string) string {
	if k == "YH_DB_PATH" {
		return a.path
	}
	return ""
}

func (a adminEnv) run(args ...string) (code int, out, errs string) {
	var o, e bytes.Buffer
	code = Run(args, a.env, &o, &e)
	return code, o.String(), e.String()
}

// create runs a create command that must succeed and returns the token it printed.
func (a adminEnv) create(args ...string) string {
	a.t.Helper()
	code, out, errs := a.run(args...)
	if code != ExitOK || errs != "" || !tokenRE.MatchString(out) {
		a.t.Fatalf("%v: code=%d err=%q", args, code, errs)
	}
	return strings.TrimSuffix(out, "\n")
}

// verify opens the database as the server would and verifies token.
func (a adminEnv) verify(token string) (bus.Principal, error) {
	a.t.Helper()
	d, err := db.Open(a.path)
	if err != nil {
		a.t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	p, _, err := auth.NewVerifier(d, time.Now, 15*time.Minute).Verify(context.Background(), token)
	return p, err
}

func TestTokenCreatePrintsOnlyToken(t *testing.T) {
	a := newAdmin(t)
	code, out, errs := a.run("token", "create", "--name", "ci-1", "--swarm", "crew")
	if code != ExitOK {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	if strings.Count(out, "\n") != 1 || !regexp.MustCompile(`^yhn_[a-z2-7]{52}\n$`).MatchString(out) {
		t.Fatalf("stdout is not exactly one token line (%d bytes)", len(out))
	}
	token := strings.TrimSpace(out)
	if strings.Contains(errs, token) || tokenRE.MatchString(errs) {
		t.Fatal("stderr holds a token")
	}
	if p, err := a.verify(token); err != nil || p != (bus.Principal{Kind: bus.PrincipalNode, Name: "ci-1", Swarm: "crew"}) {
		t.Fatalf("p=%+v err=%v", p, err)
	}
}

func TestTokenIsNeverStored(t *testing.T) {
	a := newAdmin(t)
	token := a.create("token", "create", "--name", "ci-1", "--swarm", "crew")
	for _, f := range []string{a.path, a.path + "-wal"} {
		if b, err := os.ReadFile(f); err == nil && bytes.Contains(b, []byte(token[4:])) {
			t.Fatalf("%s holds the token", filepath.Base(f))
		}
	}
}

func TestTokenListShowsOnlyThePrefix(t *testing.T) {
	a := newAdmin(t)
	token := a.create("token", "create", "--name", "ci-1", "--swarm", "crew")
	code, list, errs := a.run("token", "list")
	if code != ExitOK || errs != "" || tokenRE.MatchString(list) || !strings.Contains(list, token[:8]) || strings.Contains(list, token[:9]) {
		t.Fatalf("list code=%d err=%q out=%q", code, errs, list)
	}
	longest := 0
	for _, l := range regexp.MustCompile(`yhn_[a-z2-7]*`).FindAllString(list, -1) {
		longest = max(longest, len(l))
	}
	if longest != 8 {
		t.Fatalf("list shows %d characters of a token, want the first 8", longest)
	}
}

func TestTokenListShape(t *testing.T) {
	a := newAdmin(t)
	n := a.create("token", "create", "--name", "ci-1", "--swarm", "crew")
	s := a.create("token", "create-desk-starter", "--name", "mac")
	if code, _, _ := a.run("token", "revoke", "--name", "mac"); code != ExitOK {
		t.Fatal("revoke")
	}
	_, list, _ := a.run("token", "list")
	lines := strings.Split(strings.TrimSuffix(list, "\n"), "\n")
	if len(lines) != 3 || strings.Join(strings.Fields(lines[0]), " ") != "NAME KIND SWARM PREFIX CREATED LAST_USED REVOKED" {
		t.Fatalf("list = %q", list)
	}
	node, starter := strings.Split(lines[1], "\t"), strings.Split(lines[2], "\t")
	if len(node) != 7 || len(starter) != 7 {
		t.Fatalf("rows have %d and %d columns, want 7", len(node), len(starter))
	}
	if got, want := []string{node[0], node[1], node[2], node[3], node[5], node[6]}, []string{"ci-1", "node", "crew", n[:8], "-", "-"}; !slices.Equal(got, want) {
		t.Fatalf("node row = %q", got)
	}
	if got, want := []string{starter[0], starter[1], starter[2], starter[3], starter[5]}, []string{"mac", "desk-starter", "-", s[:8], "-"}; !slices.Equal(got, want) {
		t.Fatalf("starter row = %q", got)
	}
	for _, col := range []string{node[4], starter[4], starter[6]} {
		if _, err := time.Parse(time.RFC3339, col); err != nil {
			t.Fatalf("%q is not an RFC 3339 time", col)
		}
	}
}

func TestTokenNamesValidated(t *testing.T) {
	cases := [][]string{
		{"token", "create", "--name", "ci-1", "--swarm", "Client Co"},
		{"token", "create", "--name", "CI 1", "--swarm", "crew"},
		{"token", "create", "--name", "ci-1"},
		{"token", "create", "--swarm", "crew"},
		{"token", "create", "--name", "ci-1", "--swarm", "duet"},
		{"token", "create", "--name", "ci-1", "--swarm", "human"},
		{"token", "create-desk-starter"},
		{"token", "create-desk-starter", "--name", "Mac Book"},
		{"token", "create", "--name", "ci-1", "--swarm", "crew", "extra"},
		{"token", "create", "--bogus"},
		{"token", "revoke"},
		{"token", "revoke", "--name", "BAD NAME"},
		{"token", "revoke", "--name", "ci-1", "--desk-all"},
		{"token", "revoke", "--desk-all", "--duet-all"},
		{"token", "list", "extra"},
		{"token"},
		{"token", "bogus"},
	}
	for _, args := range cases {
		a := newAdmin(t)
		code, out, errs := a.run(args...)
		if code != ExitUsage || out != "" || !strings.Contains(errs, "yo-human-hosted") || tokenRE.MatchString(errs) {
			t.Fatalf("%v: code=%d out=%q err=%q, want 64 and no output", args, code, out, errs)
		}
		if _, err := os.Stat(a.path); err == nil {
			t.Fatalf("%v: a refused command created the database", args)
		}
	}
}

func TestTokenHelpExitsZero(t *testing.T) {
	a := newAdmin(t)
	if code, out, _ := a.run("token", "create", "-h"); code != ExitOK || out != "" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestTokenCreateRefusesDuplicateName(t *testing.T) {
	a := newAdmin(t)
	a.create("token", "create", "--name", "ci-1", "--swarm", "crew")
	code, out, errs := a.run("token", "create", "--name", "ci-1", "--swarm", "other")
	if code != ExitFailed || out != "" || !strings.Contains(errs, "already") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
}

func TestDeskStarterScopeAndCap(t *testing.T) {
	a := newAdmin(t)
	s1 := a.create("token", "create-desk-starter", "--name", "mac")
	a.create("token", "create-desk-starter", "--name", "air")
	if !strings.HasPrefix(s1, "yhd_") {
		t.Fatal("a starter is not yhd_")
	}
	// Verify gives a starter desk_start and no swarm: the 403 on /mcp is Task 9's, which holds
	// the route; here the principal carries no swarm and is no node.
	p, err := a.verify(s1)
	if err != nil || p != (bus.Principal{Kind: bus.PrincipalDeskStart, Name: "mac"}) {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	code, out, errs := a.run("token", "create-desk-starter", "--name", "third")
	if code != ExitFailed || out != "" || !strings.Contains(errs, "2") {
		t.Fatalf("third starter: code=%d out=%q err=%q", code, out, errs)
	}
}

func TestDeskAllRevokesStarters(t *testing.T) {
	a := newAdmin(t)
	s1 := a.create("token", "create-desk-starter", "--name", "mac")
	s2 := a.create("token", "create-desk-starter", "--name", "air")
	if code, out, errs := a.run("token", "revoke", "--desk-all"); code != ExitOK || out != "" || errs != "" {
		t.Fatalf("--desk-all: code=%d out=%q err=%q", code, out, errs)
	}
	for _, s := range []string{s1, s2} {
		if _, err := a.verify(s); !errors.Is(err, auth.ErrInvalid) {
			t.Fatalf("a starter after --desk-all: err=%v", err)
		}
	}
	a.create("token", "create-desk-starter", "--name", "third")
}

func TestNodeTokenNeverStartsDesk(t *testing.T) {
	a := newAdmin(t)
	n := a.create("token", "create", "--name", "ci-1", "--swarm", "crew")
	if p, err := a.verify(n); err != nil || p.Kind == bus.PrincipalDeskStart || p.Kind == bus.PrincipalDesk {
		t.Fatalf("p=%+v err=%v", p, err)
	}
}

func TestRevokedDeskTokenInvalid(t *testing.T) {
	a := newAdmin(t)
	a.create("token", "create", "--name", "ci-1", "--swarm", "crew")
	d, err := db.Open(a.path)
	if err != nil {
		t.Fatal(err)
	}
	token, hash, _ := auth.Mint(auth.PrefixDesk)
	if err := d.InsertDesk(hash, time.Now()); err != nil {
		t.Fatal(err)
	}
	_ = d.Close()
	if p, err := a.verify(token); err != nil || p.Kind != bus.PrincipalDesk {
		t.Fatalf("a live desk token: p=%+v err=%v", p, err)
	}
	if code, _, errs := a.run("token", "revoke", "--desk-all"); code != ExitOK {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	_, revoked := a.verify(token)
	unknown, _, _ := auth.Mint(auth.PrefixDesk)
	_, unk := a.verify(unknown)
	if !errors.Is(revoked, auth.ErrInvalid) || !errors.Is(unk, auth.ErrInvalid) || revoked.Error() != unk.Error() {
		t.Fatalf("revoked=%v unknown=%v, want the same ErrInvalid", revoked, unk)
	}
}

func TestTokenRevoke(t *testing.T) {
	a := newAdmin(t)
	n := a.create("token", "create", "--name", "ci-1", "--swarm", "crew")
	keep := a.create("token", "create", "--name", "ci-2", "--swarm", "crew")
	if code, out, errs := a.run("token", "revoke", "--name", "ci-1"); code != ExitOK || out != "" || errs != "" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	if _, err := a.verify(n); !errors.Is(err, auth.ErrInvalid) {
		t.Fatalf("a revoked node token: err=%v", err)
	}
	if _, err := a.verify(keep); err != nil {
		t.Fatalf("revoke --name ci-1 revoked ci-2: %v", err)
	}
	code, _, errs := a.run("token", "revoke", "--name", "ci-1")
	if code != ExitFailed || !strings.Contains(errs, "no live token") {
		t.Fatalf("revoking twice: code=%d err=%q", code, errs)
	}
	if code, _, _ := a.run("token", "revoke", "--duet-all"); code != ExitOK {
		t.Fatalf("--duet-all code=%d", code)
	}
	if _, err := a.verify(keep); err != nil {
		t.Fatalf("--duet-all revoked a node: %v", err)
	}
}

func TestAdminDatabasePath(t *testing.T) {
	a := newAdmin(t)
	other := filepath.Join(t.TempDir(), "data", "other.db")
	code, out, _ := a.run("token", "create", "--db", other, "--name", "ci-1", "--swarm", "crew")
	if code != ExitOK || !tokenRE.MatchString(out) {
		t.Fatalf("code=%d", code)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("--db did not choose the database")
	}
	if _, err := os.Stat(a.path); err == nil {
		t.Fatal("the environment's database was created despite --db")
	}
	// With neither, the default is /data/bus.db.
	if got := dbPath(func(string) string { return "" }, ""); got != "/data/bus.db" {
		t.Fatalf("default path = %q", got)
	}
}

func TestAdminStoreFailureExitsStore(t *testing.T) {
	loose := filepath.Join(t.TempDir(), "loose")
	if err := os.Mkdir(loose, 0o755); err != nil { //nolint:gosec // the test needs a loose directory
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o755); err != nil { //nolint:gosec // the test needs a loose directory
		t.Fatal(err)
	}
	var o, e bytes.Buffer
	env := func(k string) string {
		if k == "YH_DB_PATH" {
			return filepath.Join(loose, "bus.db")
		}
		return ""
	}
	code := Run([]string{"token", "create", "--name", "ci-1", "--swarm", "crew"}, env, &o, &e)
	if code != ExitStore || o.Len() != 0 || !strings.Contains(e.String(), "looser than 0700") {
		t.Fatalf("code=%d out=%q err=%q", code, o.String(), e.String())
	}
}
