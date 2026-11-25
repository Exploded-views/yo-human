package web_test

import (
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

func TestInboxAndItemShowDeliveryOnlyWhileItIsNotHealthy(t *testing.T) {
	h := newHarness(t)
	it := h.seed(nodeP, question("q1"))
	cs := h.session(h.duet)
	for _, call := range []struct {
		tool string
		args obj
	}{{"inbox_for_claude", obj{}}, {"item", obj{"id": it.ID}}} {
		if _, has := mustOK(t, cs, call.tool, call.args)["delivery"]; has {
			t.Errorf("%s shows delivery while it is healthy", call.tool)
		}
	}
	since := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	eq(t, "raising the flag", h.d.Tx(ctx, func(x bus.Tx) error {
		return x.PutBrokenFlag(bus.BrokenFlag{Raised: true, Since: since, Status: 401})
	}), error(nil))
	want := obj{"broken": true, "since": "2026-10-28T09:00:00Z", "status": float64(401), "failed": float64(0), "pending": float64(0)}
	for _, call := range []struct {
		tool string
		args obj
	}{{"inbox_for_claude", obj{}}, {"item", obj{"id": it.ID}}} {
		eq(t, call.tool+" delivery", mustOK(t, cs, call.tool, call.args)["delivery"], any(want))
	}
}
