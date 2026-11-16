package db_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

func TestNodeRoundTrip(t *testing.T) {
	d, _ := openTemp(t)
	if err := d.InsertNode("ci-1", "crew", "yhn_abcd", "hash-n", t0); err != nil {
		t.Fatal(err)
	}
	n, ok, err := d.NodeByHash("hash-n")
	want := bus.Node{Name: "ci-1", Swarm: "crew", Created: t0}
	if err != nil || !ok || n != want {
		t.Fatalf("n=%+v ok=%v err=%v", n, ok, err)
	}
	if _, ok, err := d.NodeByHash("other"); ok || err != nil {
		t.Fatalf("an unknown hash: ok=%v err=%v", ok, err)
	}
}

func TestNodeHashIsNoOtherKind(t *testing.T) {
	d, _ := openTemp(t)
	if err := d.InsertNode("ci-1", "crew", "yhn_abcd", "hash-n", t0); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := d.StarterByHash("hash-n"); ok {
		t.Fatal("a node hash found as a starter")
	}
	if _, ok, _ := d.DeskByHash("hash-n"); ok {
		t.Fatal("a node hash found as a desk token")
	}
	if _, ok, err := d.AccessByHash("hash-n"); ok || err != nil {
		t.Fatalf("access store is Task 10's: ok=%v err=%v", ok, err)
	}
}

func TestTouchNode(t *testing.T) {
	d, _ := openTemp(t)
	if err := d.InsertNode("ci-1", "crew", "yhn_abcd", "hash-n", t0); err != nil {
		t.Fatal(err)
	}
	if err := d.TouchNode("ci-1", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	n, _, _ := d.NodeByHash("hash-n")
	if !n.LastUsed.Equal(t0.Add(time.Hour)) {
		t.Fatalf("LastUsed = %v", n.LastUsed)
	}
}

func TestLiveNamesAreUnique(t *testing.T) {
	d, _ := openTemp(t)
	if err := d.InsertNode("ci-1", "crew", "yhn_abcd", "h1", t0); err != nil {
		t.Fatal(err)
	}
	if err := d.InsertNode("ci-1", "other", "yhn_abcd", "h2", t0); !db.IsConflict(err) {
		t.Fatalf("a second live ci-1: err=%v, want a conflict", err)
	}
	// One name space for nodes and starters, so `revoke --name` is never ambiguous.
	if err := d.InsertStarter("ci-1", "yhd_abcd", "h3", t0, 2); !db.IsConflict(err) {
		t.Fatalf("a starter named like a node: err=%v, want a conflict", err)
	}
	if err := d.InsertNode("ci-2", "crew", "yhn_abcd", "h1", t0); !db.IsConflict(err) {
		t.Fatalf("a repeated hash: err=%v, want a conflict", err)
	}
	if ok, err := d.Revoke("ci-1", t0.Add(time.Minute)); err != nil || !ok {
		t.Fatalf("Revoke: ok=%v err=%v", ok, err)
	}
	if err := d.InsertNode("ci-1", "crew", "yhn_wxyz", "h4", t0.Add(2*time.Minute)); err != nil {
		t.Fatalf("a name is free again once its token is revoked: %v", err)
	}
}

func TestRevoke(t *testing.T) {
	d, _ := openTemp(t)
	_ = d.InsertNode("ci-1", "crew", "yhn_abcd", "h1", t0)
	_ = d.InsertStarter("mac", "yhd_abcd", "h2", t0, 2)
	at := t0.Add(time.Minute)
	if ok, err := d.Revoke("nobody", at); ok || err != nil {
		t.Fatalf("unknown name: ok=%v err=%v", ok, err)
	}
	for _, name := range []string{"ci-1", "mac"} {
		if ok, err := d.Revoke(name, at); !ok || err != nil {
			t.Fatalf("Revoke(%s): ok=%v err=%v", name, ok, err)
		}
	}
	n, _, _ := d.NodeByHash("h1")
	s, _, _ := d.StarterByHash("h2")
	if !n.Revoked.Equal(at) || !s.Revoked.Equal(at) {
		t.Fatalf("revoked: node %v, starter %v", n.Revoked, s.Revoked)
	}
	// A second revoke finds nothing live and leaves the first time alone.
	if ok, _ := d.Revoke("ci-1", at.Add(time.Hour)); ok {
		t.Fatal("a revoked token was revoked again")
	}
	n, _, _ = d.NodeByHash("h1")
	if !n.Revoked.Equal(at) {
		t.Fatalf("Revoked moved to %v", n.Revoked)
	}
}

func TestStarterCapAndDeskAll(t *testing.T) {
	d, _ := openTemp(t)
	for i, name := range []string{"mac", "air"} {
		if err := d.InsertStarter(name, "yhd_abcd", "hs"+name, t0, 2); err != nil {
			t.Fatalf("starter %d: %v", i, err)
		}
	}
	if err := d.InsertStarter("third", "yhd_abcd", "hs3", t0, 2); !errors.Is(err, db.ErrStarterCap) {
		t.Fatalf("a third live starter: err=%v, want ErrStarterCap", err)
	}
	if _, ok, _ := d.StarterByHash("hs3"); ok {
		t.Fatal("the refused starter was stored")
	}
}

func TestStarterCapIgnoresNodesAndRevoked(t *testing.T) {
	d, _ := openTemp(t)
	for _, name := range []string{"mac", "air"} {
		if err := d.InsertStarter(name, "yhd_abcd", "hs"+name, t0, 2); err != nil {
			t.Fatal(err)
		}
	}
	_ = d.InsertNode("ci-1", "crew", "yhn_abcd", "hn", t0)
	if ok, _ := d.Revoke("mac", t0.Add(time.Minute)); !ok {
		t.Fatal("revoke mac")
	}
	if err := d.InsertStarter("third", "yhd_abcd", "hs3", t0, 2); err != nil {
		t.Fatalf("after a revoke: %v", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// revokedAt returns when the credential with hash h was revoked, in the store of its kind.
func revokedAt(t *testing.T, d *db.DB, kind, h string) time.Time {
	t.Helper()
	var r time.Time
	var ok bool
	var err error
	switch kind {
	case "node":
		var n bus.Node
		n, ok, err = d.NodeByHash(h)
		r = n.Revoked
	case "starter":
		var s bus.Starter
		s, ok, err = d.StarterByHash(h)
		r = s.Revoked
	default:
		var dk bus.Desk
		dk, ok, err = d.DeskByHash(h)
		r = dk.Revoked
	}
	if err != nil || !ok {
		t.Fatalf("%s %s: ok=%v err=%v", kind, h, ok, err)
	}
	return r
}

func wantRevoked(t *testing.T, d *db.DB, kind, h string, want time.Time) {
	t.Helper()
	if got := revokedAt(t, d, kind, h); !got.Equal(want) {
		t.Fatalf("%s %s revoked at %v, want %v", kind, h, got, want)
	}
}

func TestRevokeDeskRevokesStartersAndDeskTokens(t *testing.T) {
	d, _ := openTemp(t)
	for _, name := range []string{"mac", "air", "third"} {
		must(t, d.InsertStarter(name, "yhd_abcd", "hs"+name, t0, 3))
	}
	must(t, d.InsertNode("ci-1", "crew", "yhn_abcd", "hn", t0))
	must(t, d.InsertDesk("hd1", t0))
	must(t, d.InsertDesk("hd2", t0))
	early, at := t0.Add(time.Minute), t0.Add(time.Hour)
	if ok, _ := d.Revoke("mac", early); !ok {
		t.Fatal("revoke mac")
	}
	starters, desks, err := d.RevokeDesk(at)
	if err != nil || starters != 2 || desks != 2 {
		t.Fatalf("RevokeDesk = %d starters, %d desk tokens, err=%v; want 2 live starters and 2 desk tokens", starters, desks, err)
	}
	wantRevoked(t, d, "desk", "hd1", at)
	wantRevoked(t, d, "desk", "hd2", at)
	wantRevoked(t, d, "starter", "hsair", at)
	wantRevoked(t, d, "starter", "hsthird", at)
	wantRevoked(t, d, "node", "hn", time.Time{})
	wantRevoked(t, d, "starter", "hsmac", early)
	if starters, desks, _ = d.RevokeDesk(at.Add(time.Hour)); starters != 0 || desks != 0 {
		t.Fatalf("a second RevokeDesk revoked %d and %d", starters, desks)
	}
}

func TestRevokeDuetWithNoAccessStore(t *testing.T) {
	d, _ := openTemp(t)
	if n, err := d.RevokeDuet(t0); err != nil || n != 0 {
		t.Fatalf("RevokeDuet = %d, %v", n, err)
	}
}

func TestListTokensHoldsNoHash(t *testing.T) {
	d, _ := openTemp(t)
	_ = d.InsertStarter("mac", "yhd_wxyz", "SECRET-HASH-FAKE-2", t0.Add(time.Second), 2)
	_ = d.InsertNode("ci-1", "crew", "yhn_abcd", "SECRET-HASH-FAKE-1", t0)
	_ = d.TouchNode("ci-1", t0.Add(time.Hour))
	_, _ = d.Revoke("mac", t0.Add(2*time.Hour))
	rows, err := d.ListTokens()
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	want := []bus.TokenRow{
		{Kind: bus.TokenNode, Name: "ci-1", Swarm: "crew", Shown: "yhn_abcd", Created: t0, LastUsed: t0.Add(time.Hour)},
		{Kind: bus.TokenStarter, Name: "mac", Shown: "yhd_wxyz", Created: t0.Add(time.Second), Revoked: t0.Add(2 * time.Hour)},
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("row %d = %+v, want %+v", i, rows[i], want[i])
		}
	}
	for _, r := range rows {
		if strings.Contains(strings.Join([]string{string(r.Kind), r.Name, r.Swarm, r.Shown}, "|"), "HASH") {
			t.Fatalf("a listed row holds a hash: %+v", r)
		}
	}
}

func TestDeskRoundTrip(t *testing.T) {
	d, _ := openTemp(t)
	if err := d.InsertDesk("hd", t0); err != nil {
		t.Fatal(err)
	}
	if err := d.InsertDesk("hd", t0); !db.IsConflict(err) {
		t.Fatalf("a repeated desk hash: err=%v", err)
	}
	dk, ok, err := d.DeskByHash("hd")
	if err != nil || !ok || dk != (bus.Desk{Created: t0}) {
		t.Fatalf("dk=%+v ok=%v err=%v", dk, ok, err)
	}
	if _, ok, _ := d.DeskByHash("nope"); ok {
		t.Fatal("an unknown desk hash was found")
	}
}

func TestTokenTablesRefuseBadRows(t *testing.T) {
	d, _ := openTemp(t)
	// The schema's own refusals, behind the code's: an empty swarm on a node, a swarm on a starter.
	if err := d.InsertNode("ci-1", "", "yhn_abcd", "h", t0); err == nil {
		t.Fatal("a node with no swarm was stored")
	}
}
