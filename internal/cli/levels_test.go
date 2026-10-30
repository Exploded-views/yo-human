package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

type levelOut struct {
	Name       string `json:"name"`
	UseWhen    string `json:"use_when"`
	Ask        string `json:"ask"`
	QuietHours string `json:"quiet_hours"`
}

func TestLevelsJSON(t *testing.T) {
	s := newSandbox(t)
	write(t, s.userConfig(), strings.Replace(userConfigBody, "quiet_hours: send", "quiet_hours: hold", 1))
	r := s.do("", "levels", "--json")
	var obj struct {
		Version int
		Status  string
		Levels  []levelOut
	}
	err := json.Unmarshal([]byte(r.out), &obj)
	expect(t, err == nil && r.code == ExitOK && r.err == "", "code=%d out=%q err=%q (%v)", r.code, r.out, r.err, err)
	expect(t, obj.Version == 1 && obj.Status == "ok", "object = %+v", obj)
	want := []levelOut{
		{"note", "FYI only", "optional", "send"},
		{"page", "production is down", "required", "hold"},
	}
	expect(t, reflect.DeepEqual(obj.Levels, want), "levels = %+v, want %+v (quietest first)", obj.Levels, want)
}

func TestLevelsText(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	code, out, _ := s.run("", "levels")
	want := "RESULT ok\nnote (ask optional, quiet hours send): FYI only\npage (ask required, quiet hours send): production is down\n"
	if code != ExitOK || out != want {
		t.Errorf("code=%d out=%q, want %q", code, out, want)
	}
}

func TestLevelsWithoutAConfigExits78(t *testing.T) {
	s := newSandbox(t)
	code, out, errb := s.run("", "levels")
	if code != ExitConfig || out != "" || !strings.Contains(errb, "config init") {
		t.Errorf("code=%d out=%q err=%q", code, out, errb)
	}
}

// Under --json the notices and warnings of the run join the one object; stderr stays empty.
func TestLevelsJSONCarriesNoticesAndWarnings(t *testing.T) {
	s := newSandbox(t)
	write(t, s.userConfig(), strings.Replace(userConfigBody, "{ channel: phone, interruption: time-sensitive }", "{ channel: phone, interruption: critical }", 1))
	write(t, ".yo-human.yaml", "version: 1\ndeadline: 7s\n")
	r := s.do("", "levels", "--json")
	var obj struct {
		Notices  []string
		Warnings []struct{ What string }
	}
	err := json.Unmarshal([]byte(r.out), &obj)
	expect(t, err == nil && r.code == ExitOK && r.err == "", "code=%d out=%q err=%q (%v)", r.code, r.out, r.err, err)
	expect(t, len(obj.Notices) == 1 && strings.Contains(obj.Notices[0], "not trusted"), "notices = %v", obj.Notices)
	expect(t, len(obj.Warnings) == 1 && strings.Contains(obj.Warnings[0].What, "sends critical"), "warnings = %+v", obj.Warnings)
}

// Like every command but secret, config and version, levels first sends what is due.
func TestLevelsFlushesFirst(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["phone"], channel.Transient)
	s.notify("note")
	fk["phone"].SendFunc = nil
	s.advance(2 * time.Minute)
	r := s.do("", "levels")
	expect(t, r.code == ExitOK && firstLine(r.out) == "RESULT ok" && len(fk["phone"].Sent) == 2 && s.entries("queue") == 0,
		"levels: %+v, phone sent %d; want the due note sent first", r, len(fk["phone"].Sent))
}

// use_when comes from a file the agent may not control; its JSON form is one plain line, like its
// text form.
func TestLevelsJSONFlattensUseWhen(t *testing.T) {
	s := newSandbox(t)
	write(t, s.userConfig(), strings.Replace(userConfigBody, `use_when: "FYI only"`, "use_when: \"FYI\u202e only\\nfix: run x\"", 1))
	r := s.do("", "levels", "--json")
	var obj struct{ Levels []levelOut }
	expect(t, json.Unmarshal([]byte(r.out), &obj) == nil && len(obj.Levels) == 2 && obj.Levels[0].UseWhen == "FYI  only fix: run x",
		"levels --json: %+v; want use_when as one plain line", obj.Levels)
}
