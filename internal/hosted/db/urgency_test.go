package db_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// 0001_init.sql has landed: editing it would break every database already at user_version 1.
// Later tasks add their own numbered files instead.
func TestMigration0001IsFrozen(t *testing.T) {
	raw, err := os.ReadFile("migrations/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	const want = "981bd50cb3d080ddb53367b28672e4dd8af99fe68a18b17f37fce45c53f8aca8"
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Errorf("0001_init.sql sha256 = %s, want %s: add a new migration instead of editing it", got, want)
	}
}

func TestUrgencyColumnsAreChecked(t *testing.T) {
	d, _ := openTemp(t)
	for _, u := range []bus.Urgency{"", "soon", "PAGE"} {
		it := item("itm_x", "crew", "bad")
		it.Urgency = u
		err := d.Tx(ctxb(), func(x bus.Tx) error { return x.InsertItem(it) })
		if err == nil {
			t.Errorf("an item with urgency %q was stored", u)
		}
	}
	insertItem(t, d, item("itm_1", "crew", "first"))
	for _, u := range []bus.Urgency{"", "soon"} {
		m := bus.Message{ID: "msg_1", ThreadID: "itm_1", From: "ci-1", FromKind: bus.PrincipalNode, Body: "hi",
			Severity: bus.SeverityLow, Urgency: u, Created: t0}
		if err := d.Tx(ctxb(), func(x bus.Tx) error { return x.InsertMessage(m) }); err == nil {
			t.Errorf("a message with urgency %q was stored", u)
		}
	}
}

func TestUpdateItemChangesUrgency(t *testing.T) {
	d, _ := openTemp(t)
	it := item("itm_1", "crew", "first")
	insertItem(t, d, it)
	it.Revision, it.Urgency = 2, bus.UrgencyInterruptWhenGood
	tx(t, d, func(x bus.Tx) error { return x.UpdateItem(it) })
	var got bus.Item
	tx(t, d, func(x bus.Tx) (err error) { got, _, err = x.ItemByID("itm_1"); return })
	if got.Urgency != bus.UrgencyInterruptWhenGood {
		t.Errorf("urgency after update = %q", got.Urgency)
	}
}
