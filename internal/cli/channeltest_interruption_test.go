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
	want := `{"version":1,"status":"delivered","id":"` + id + `","channels":[{"channel":"phone","status":"sent"}]}` + "\n"
	expect(t, strings.Replace(r.out, jsonID(r.out), id, 1) == want, "json = %q; want %q with the id substituted", r.out, want)
	expect(t, len(fk["phone"].Specs) == 2 && fk["phone"].Specs[0].Interruption == channel.Passive && fk["phone"].Specs[1].Interruption == channel.Passive, "specs = %+v; want passive", fk["phone"].Specs)
}

// A level above passive reaches the channel as asked, and the result names the key the provider used.
func TestChannelTestSendsTheRequestedInterruption(t *testing.T) {
	for _, level := range []string{"active", "time-sensitive", "critical"} {
		name := "pager" // pagerduty sends all four and has incidents, so it shows a key
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
	mustNotContain(t, "text", r.out, "resolve --dedup-key", "next:")
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
		s.do("", "channel", "test", "pager", "--interruption", v).fails(t, "value "+v, ExitUsage, "not one of the four interruptions")
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

// jsonID is the "id" of a result object.
func jsonID(out string) string {
	var m struct{ ID string }
	_ = json.Unmarshal([]byte(out), &m)
	return m.ID
}

// A key and a close line belong to a channel type with incidents (PagerDuty). ntfy, Bark and the
// log have none, so they print neither and carry no dedup_key.
func TestChannelTestShowsAKeyOnlyWhereIncidentsExist(t *testing.T) {
	s, fk := notifySandbox(t)
	write(t, s.userConfig(), strings.Replace(notifyConfigBody, "  journal:", `  bell: { type: bark, server: "https://bark.example", device_key: { secret: bark-key } }
  journal:`, 1))
	s.setSecret("bark-key")
	fk["bell"] = s.useFakes("journal", "phone", "pager", "bell")["bell"]
	for _, c := range []struct{ name, level string }{{"phone", "time-sensitive"}, {"bell", "critical"}, {"journal", "critical"}} {
		r := s.do("", "channel", "test", c.name, "--interruption", c.level)
		expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT delivered "), "%s: %+v", c.name, r)
		mustNotContain(t, c.name+" text", r.out, "key:", "close:")
		j := s.do("", "channel", "test", c.name, "--interruption", c.level, "--json")
		mustContain(t, c.name+" json", j.out, `"interruption":"`+c.level+`"`)
		mustNotContain(t, c.name+" json", j.out, `"dedup_key"`)
	}
	expect(t, len(fk["bell"].Specs) == 2 && fk["bell"].Specs[0].Interruption == channel.Critical, "bark specs = %+v; want critical, sent", fk["bell"].Specs)
}

// A refusal must not lead a caller to a louder test.
func TestChannelTestRefusalSteersNoOneLouder(t *testing.T) {
	s, _ := notifySandbox(t)
	r := s.do("", "channel", "test", "phone", "--interruption", "critical")
	mustContain(t, "stderr", r.err, "only the human runs channel test above passive", "leaves --interruption out", "valid: an interruption it supports: passive, active, time-sensitive")
	mustNotContain(t, "stderr", r.err, "pass one of those")
}

// An unknown value is refused the same way: its fix names no louder value to try.
func TestChannelTestUnknownValueSteersNoOneLouder(t *testing.T) {
	s, _ := notifySandbox(t)
	r := s.do("", "channel", "test", "pager", "--interruption", "loud")
	mustContain(t, "stderr", r.err, "only the human runs channel test above passive", "leaves --interruption out")
	mustNotContain(t, "stderr", r.err, "fix: pass --interruption")
}

func TestChannelTestHelpPrintsUsage(t *testing.T) {
	s, fk := notifySandbox(t)
	for _, args := range [][]string{{"channel", "test", "pager", "--help"}, {"channel", "test", "pager", "-h"}, {"channel", "test", "--interruption", "critical", "pager", "-h"}} {
		r := s.do("", args...)
		expect(t, r.code == ExitOK && strings.Contains(r.out+r.err, "usage: yo-human"), "%v: %+v; want usage and exit 0", args, r)
	}
	expect(t, len(fk["pager"].Sent) == 0, "help sent something")
}
