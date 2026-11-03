package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

// Without the flag the output is exactly what it was before the flag existed.
func TestChannelTestWithoutTheFlagPrintsExactlyWhatItAlwaysDid(t *testing.T) {
	s, fk := notifySandbox(t)
	r := s.do("", "channel", "test", "phone")
	id := strings.TrimPrefix(firstLine(r.out), "RESULT delivered ")
	expect(t, r.code == ExitOK && r.err == "" && r.out == "RESULT delivered "+id+"\nphone: sent\n", "text = %+v; want the RESULT line and the channel line only", r)
	r = s.do("", "channel", "test", "phone", "--json")
	var m map[string]any
	expect(t, json.Unmarshal([]byte(r.out), &m) == nil, "json = %q", r.out)
	mustNotContain(t, "json", r.out, `"interruption"`, `"dedup_key"`)
	expect(t, len(fk["phone"].Specs) == 2 && fk["phone"].Specs[0].Interruption == channel.Passive && fk["phone"].Specs[1].Interruption == channel.Passive, "specs = %+v; want passive", fk["phone"].Specs)
}

// A level above passive reaches the channel as asked, and the result names the key the provider used.
func TestChannelTestSendsTheRequestedInterruption(t *testing.T) {
	for _, level := range []string{"active", "time-sensitive", "critical"} {
		name := "pager"
		if level != "critical" {
			name = "phone" // ntfy has no critical; pagerduty has them all
		}
		for _, args := range [][]string{
			{"channel", "test", name, "--interruption", level},
			{"channel", "test", "--interruption", level, name},
			{"channel", "test", "--interruption=" + level, name},
		} {
			checkInterruptionSent(t, name, level, args)
		}
	}
}

func checkInterruptionSent(t *testing.T, name, level string, args []string) {
	t.Helper()
	s, fk := notifySandbox(t)
	r := s.do("", args...)
	f := fk[name]
	expect(t, r.code == ExitOK && r.err == "" && len(f.Sent) == 1, "%v: %+v; want one send", args, r)
	if len(f.Sent) != 1 {
		return
	}
	expect(t, string(f.Specs[0].Interruption) == level && f.Specs[0].Instance == name, "%v: spec = %+v; want %s", args, f.Specs[0], level)
	expect(t, strings.Contains(strings.ToLower(f.Sent[0].Title), "test") && f.Sent[0].Ask != "", "%v: message = %+v; want a test message", args, f.Sent[0])
	id := f.Sent[0].ID
	mustContain(t, "text", r.out, "RESULT delivered "+id, name+": sent", "key: "+id, "resolve the incident in the provider")
	mustNotContain(t, "text", r.out, "yo-human resolve")
	j := s.do("", append(append([]string{}, args...), "--json")...)
	if len(f.Sent) != 2 {
		t.Errorf("%v: the json run sent %d; want 2 in all", args, len(f.Sent))
		return
	}
	var m map[string]any
	expect(t, json.Unmarshal([]byte(j.out), &m) == nil && m["interruption"] == level && m["dedup_key"] == f.Sent[1].ID, "%v: json = %q; want interruption %s and dedup_key %s", args, j.out, level, f.Sent[1].ID)
}

func TestChannelTestPassiveGivenExplicitlyHasNoKey(t *testing.T) {
	s, fk := notifySandbox(t)
	r := s.do("", "channel", "test", "phone", "--interruption", "passive", "--json")
	var m map[string]any
	expect(t, json.Unmarshal([]byte(r.out), &m) == nil && m["interruption"] == "passive", "json = %q; want interruption passive", r.out)
	mustNotContain(t, "json", r.out, `"dedup_key"`)
	r = s.do("", "channel", "test", "phone", "--interruption", "passive")
	expect(t, strings.Count(r.out, "\n") == 2 && !strings.Contains(r.out, "key:"), "text = %q; want no key line", r.out)
	expect(t, len(fk["phone"].Specs) == 2 && fk["phone"].Specs[0].Interruption == channel.Passive, "specs = %+v; want two passive sends", fk["phone"].Specs)
}

// A channel that cannot send the interruption is refused before anything is sent, after the flush.
func TestChannelTestRefusesAnInterruptionTheChannelLacks(t *testing.T) {
	s, fk := notifySandbox(t)
	r := s.do("", "channel", "test", "phone", "--interruption", "critical")
	r.fails(t, "ntfy critical", ExitUsage, "passive, active, time-sensitive")
	mustContain(t, "stderr", r.err, "phone")
	expect(t, len(fk["phone"].Sent) == 0, "the refused test sent something")
	r = s.do("", "channel", "test", "phone", "--interruption", "critical", "--json")
	expect(t, r.code == ExitUsage && r.err == "" && strings.Contains(r.out, "passive, active, time-sensitive"), "json = %+v", r)
	s.do("", "channel", "test", "journal", "--interruption", "critical") // log supports it, so this one is sent
	expect(t, len(fk["journal"].Sent) == 1, "journal sent %d; want 1", len(fk["journal"].Sent))
}

func TestChannelTestRefusalComesAfterTheFlush(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["pager"], channel.Transient)
	s.notify("page", "--title", "older")
	fk["pager"].SendFunc = nil
	s.advance(2 * time.Minute)
	n := len(fk["pager"].Sent)
	r := s.do("", "channel", "test", "phone", "--interruption", "critical")
	expect(t, r.code == ExitUsage && len(fk["pager"].Sent) == n+1, "%+v; pager got %d new sends; want the due page flushed before the refusal", r, len(fk["pager"].Sent)-n)
}

func TestChannelTestBadInterruptionIsAUsageError(t *testing.T) {
	s, fk := notifySandbox(t)
	for _, v := range []string{"loud", "", "Critical", "passive,critical"} {
		s.do("", "channel", "test", "pager", "--interruption", v).fails(t, "value "+v, ExitUsage, "passive, active, time-sensitive, critical")
	}
	s.do("", "channel", "test", "pager", "--interruption").fails(t, "no value", ExitUsage, "--interruption")
	s.do("", "channel", "test", "--interruption", "critical").fails(t, "no name", ExitUsage, "takes one channel name")
	s.do("", "channel", "test", "pager", "phone", "--interruption", "critical").fails(t, "two names", ExitUsage, "takes one channel name")
	s.do("", "channel", "test", "pager", "--bogus").fails(t, "unknown flag", ExitUsage, "--interruption")
	expect(t, len(fk["pager"].Sent) == 0, "a refused call sent something")
}

// A repeated flag takes the last value, as every other flag does.
func TestChannelTestRepeatedFlagTakesTheLast(t *testing.T) {
	s, fk := notifySandbox(t)
	s.do("", "channel", "test", "pager", "--interruption", "critical", "--interruption", "active")
	expect(t, len(fk["pager"].Specs) == 1 && fk["pager"].Specs[0].Interruption == channel.Active, "specs = %+v; want active", fk["pager"].Specs)
}
