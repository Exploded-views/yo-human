package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
)

// withoutPagerSecret is the notify sandbox with pd-critical unset: the pager cannot be built.
func withoutPagerSecret(t *testing.T) (*sandbox, fakeSet) {
	t.Helper()
	s, fk := notifySandbox(t)
	if err := os.Remove(s.secretsFile()); err != nil {
		t.Fatal(err)
	}
	s.setSecret("ntfy-topic")
	return s, fk
}

// One channel of a level that cannot be built must not silence the rest: the phone is paged, the
// pager is reported skipped with its Config problem, and the exit is 78 so the human hears of it.
func TestALevelSendsThroughEveryChannelThatBuilds(t *testing.T) {
	s, fk := withoutPagerSecret(t)
	r := s.notify("page")
	expect(t, r.code == ExitConfig && strings.HasPrefix(r.out, "RESULT partial "), "page: %+v; want the result, then exit 78", r)
	mustContain(t, "stdout", r.out, "phone: sent", "pager: skipped")
	mustContain(t, "stderr", r.err, "pd-critical", "yo-human secret set pd-critical")
	expect(t, len(fk["phone"].Sent) == 1 && len(fk["pager"].Sent) == 0, "phone sent %d, pager %d; want the phone paged", len(fk["phone"].Sent), len(fk["pager"].Sent))
	expect(t, s.entries("queue") == 0, "queued %d; a skipped channel is not queued", s.entries("queue"))
}

func TestALevelWithNoChannelThatBuildsFails(t *testing.T) {
	s, fk := withoutPagerSecret(t)
	r := s.notify("night", "--json")
	var obj struct {
		Status   string
		Channels []struct{ Channel, Status string }
		Errors   []struct{ What string }
	}
	expect(t, r.code == ExitConfig && json.Unmarshal([]byte(r.out), &obj) == nil, "night --json: %+v; want exit 78 and one object", r)
	expect(t, obj.Status == "failed" && len(obj.Channels) == 1 && obj.Channels[0].Status == "skipped" && len(obj.Errors) == 1 && strings.Contains(obj.Errors[0].What, "pd-critical"),
		"object %+v; want failed, the pager skipped and its problem", obj)
	expect(t, len(fk["pager"].Sent) == 0 && s.entries("queue") == 0, "nothing may be sent or queued")
}

// While another channel of the level is queued for a retry, the next step comes first (exit 75);
// the channel that could not be built is still reported, as a warning.
func TestALevelWithAQueuedChannelAndOneThatCannotBuildSaysToRetry(t *testing.T) {
	s, fk := withoutPagerSecret(t)
	failing(fk["phone"], channel.Transient)
	r := s.notify("page")
	expect(t, r.code == ExitQueued && strings.HasPrefix(r.out, "RESULT queued ") && strings.Contains(r.out, "\nnext: "), "page: %+v; want queued with a next step, exit 75", r)
	mustContain(t, "stderr", r.err, "warning: ", "pd-critical")
}
