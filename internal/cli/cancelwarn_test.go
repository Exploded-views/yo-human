package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
)

// holdPhoneBody holds "night" on the phone, which schedules at the provider, inside a window that
// never ends; "page" goes to the pager now. %s is the phone's server.
const holdPhoneBody = `version: 1
channels:
  phone: { type: ntfy, server: "%s", topic: { secret: ntfy-topic } }
  pager: { type: pagerduty, routing_key: { secret: pd-critical } }
quiet_hours:
  timezone: UTC
  windows: [ { days: [mon, tue, wed, thu, fri, sat, sun], from: "00:00", to: "00:00" } ]
levels:
  - name: night
    use_when: "can wait until morning"
    quiet_hours: hold
    send:
      - { channel: phone, interruption: active }
  - name: page
    use_when: "production is down"
    send:
      - { channel: pager, interruption: critical }
`

// raisedPastAMovedPhone holds a night message on the phone, moves the phone to another server, and
// raises the incident to page: the held copy cannot be cancelled through the moved phone.
func raisedPastAMovedPhone(t *testing.T, extra ...string) result {
	t.Helper()
	s, fakes := notifySandbox(t)
	fakes["phone"].SendFunc = func(_ context.Context, _ channel.Message, sp channel.SendSpec) (channel.Receipt, error) {
		return channel.Receipt{Instance: sp.Instance, ProviderID: "SCHED-FAKE", Scheduled: !sp.DeliverAt.IsZero()}, nil
	}
	write(t, s.userConfig(), fmt.Sprintf(holdPhoneBody, "https://one.example.com"))
	if r := s.notify("night", "--dedup-key", "k"); r.code != ExitOK {
		t.Fatalf("night: %+v", r)
	}
	write(t, s.userConfig(), fmt.Sprintf(holdPhoneBody, "https://two.example.com"))
	return s.notify("page", append([]string{"--dedup-key", "k"}, extra...)...)
}

// The page was delivered, so the call exits 0; the copy it could not cancel is a warning.
func TestAReplacedSendThatCannotBeCancelledIsAWarning(t *testing.T) {
	r := raisedPastAMovedPhone(t)
	expect(t, r.code == ExitOK && firstLine(r.out) != "" && resultLine.MatchString(firstLine(r.out)), "page: %+v; want exit 0 and a RESULT", r)
	mustContain(t, "stderr", r.err, "warning: a scheduled send on phone could not be cancelled yet")
}

func TestAReplacedSendThatCannotBeCancelledIsAWarningInJSON(t *testing.T) {
	r := raisedPastAMovedPhone(t, "--json")
	var obj struct {
		Status   string `json:"status"`
		Warnings []struct{ What string }
		Errors   []any
	}
	if err := json.Unmarshal([]byte(r.out), &obj); err != nil {
		t.Fatalf("stdout %q: %v", r.out, err)
	}
	expect(t, r.code == ExitOK && obj.Status == "delivered" && len(obj.Errors) == 0 && r.err == "", "page --json: %+v; want delivered, exit 0, no errors, nothing on stderr", r)
	expect(t, len(obj.Warnings) == 1, "warnings %+v; want the cancel", obj.Warnings)
}
