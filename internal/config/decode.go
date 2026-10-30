package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// The raw shapes mirror the file; decode turns them into the model and applies the defaults.
type (
	rawConfig struct {
		Version     int                       `yaml:"version"`
		Human       Human                     `yaml:"human"`
		Deadline    string                    `yaml:"deadline"`
		IncidentTTL string                    `yaml:"incident_ttl"`
		Channels    map[string]map[string]any `yaml:"channels"`
		Levels      []rawLevel                `yaml:"levels"`
		QuietHours  *rawQuiet                 `yaml:"quiet_hours"`
		Shim        *rawShim                  `yaml:"shim"`
	}
	rawLevel struct {
		Name       string    `yaml:"name"`
		UseWhen    string    `yaml:"use_when"`
		Ask        string    `yaml:"ask"`
		QuietHours string    `yaml:"quiet_hours"`
		Retry      rawRetry  `yaml:"retry"`
		Reminder   string    `yaml:"reminder"`
		Send       []rawSend `yaml:"send"`
	}
	rawRetry struct {
		Every string `yaml:"every"`
		Times *int   `yaml:"times"`
	}
	rawSend struct {
		Channel      string         `yaml:"channel"`
		Interruption string         `yaml:"interruption"`
		Options      map[string]any `yaml:"options"`
	}
	rawQuiet struct {
		Timezone string      `yaml:"timezone"`
		Windows  []rawWindow `yaml:"windows"`
	}
	rawWindow struct {
		Days     []string `yaml:"days"`
		From, To string
	}
	rawShim struct {
		Default ShimMode `yaml:"default"`
		Low     ShimMode `yaml:"low"`
		Agent   string   `yaml:"agent"`
	}
)

const (
	defaultDeadline    = time.Second
	defaultIncidentTTL = 24 * time.Hour
	defaultRetryEvery  = 60 * time.Second
	defaultRetryTimes  = 5
)

var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

// decoder reports a bad setting with the file it came from.
type decoder struct{ origin map[string]string }

func (d decoder) bad(setting, top, valid string) error {
	return problem.Configf("%s in %q is not valid", valid, "edit that setting in "+problem.ShellQuote(d.origin[top]), setting, d.origin[top])
}

// duration parses a Go duration; an empty string is def. A setting that must be positive refuses
// zero, and none may be negative.
func (d decoder) duration(setting, top, s string, def time.Duration, positive bool) (time.Duration, error) {
	if s == "" {
		return def, nil
	}
	v, err := time.ParseDuration(s)
	if err != nil || v < 0 || (positive && v == 0) {
		return 0, d.bad(setting, top, "a Go duration such as 1s, 30m or 24h, greater than zero")
	}
	return v, nil
}

// decode turns the merged mapping into a Config.
func decode(merged map[string]any, origin map[string]string) (*Config, error) {
	b, err := yaml.Marshal(merged)
	var raw rawConfig
	if err == nil {
		err = yaml.Unmarshal(b, &raw)
	}
	if err != nil {
		return nil, problem.Internalf("the merged config cannot be decoded", "a config that passed the schema", "report this as a bug")
	}
	d := decoder{origin: origin}
	c := &Config{Version: raw.Version, Human: raw.Human, Origin: origin, Channels: map[string]ChannelInstance{}}
	if c.Version == 0 {
		c.Version = 1
	}
	if c.Deadline, err = d.duration("deadline", "deadline", raw.Deadline, defaultDeadline, true); err != nil {
		return nil, err
	}
	if c.IncidentTTL, err = d.duration("incident_ttl", "incident_ttl", raw.IncidentTTL, defaultIncidentTTL, true); err != nil {
		return nil, err
	}
	for name, m := range raw.Channels {
		c.Channels[name] = channelInstance(m)
	}
	if c.Levels, err = d.levels(raw.Levels); err != nil {
		return nil, err
	}
	if c.QuietHours, err = d.quietHours(raw.QuietHours); err != nil {
		return nil, err
	}
	if raw.Shim != nil {
		c.Shim = &Shim{Default: raw.Shim.Default, Low: raw.Shim.Low, Agent: raw.Shim.Agent}
	}
	return c, nil
}

func channelInstance(m map[string]any) ChannelInstance {
	typ, _ := m["type"].(string)
	settings := make(map[string]any, len(m))
	for k, v := range m {
		if k != "type" {
			settings[k] = v
		}
	}
	return ChannelInstance{Type: typ, Settings: settings}
}

func (d decoder) levels(raw []rawLevel) ([]Level, error) {
	var out []Level
	for i, r := range raw {
		l, err := d.level(i, r)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

func (d decoder) level(i int, r rawLevel) (Level, error) {
	at := func(field string) string { return fmt.Sprintf("levels.%d.%s", i, field) }
	l := Level{Name: r.Name, UseWhen: r.UseWhen, AskRequired: r.Ask != "optional", HoldInQuietHours: r.QuietHours == "hold",
		Retry: Retry{Times: defaultRetryTimes}}
	var err error
	if l.Retry.Every, err = d.duration(at("retry.every"), "levels", r.Retry.Every, defaultRetryEvery, true); err != nil {
		return l, err
	}
	if r.Retry.Times != nil {
		l.Retry.Times = *r.Retry.Times
	}
	if l.Reminder, err = d.duration(at("reminder"), "levels", r.Reminder, 0, false); err != nil {
		return l, err
	}
	for j, s := range r.Send {
		in, err := channel.ParseInterruption(s.Interruption)
		if err != nil {
			return l, d.bad(fmt.Sprintf("levels.%d.send.%d.interruption", i, j), "levels", "passive, active, time-sensitive or critical")
		}
		l.Send = append(l.Send, Send{Channel: s.Channel, Interruption: in, Options: s.Options})
	}
	return l, nil
}

func (d decoder) quietHours(r *rawQuiet) (QuietHours, error) {
	if r == nil {
		return QuietHours{}, nil
	}
	loc, err := time.LoadLocation(r.Timezone)
	if err != nil {
		return QuietHours{}, d.bad("quiet_hours.timezone", "quiet_hours", "an IANA zone name such as America/New_York")
	}
	q := QuietHours{Location: loc}
	for i, rw := range r.Windows {
		w, err := d.window(i, rw)
		if err != nil {
			return QuietHours{}, err
		}
		q.Windows = append(q.Windows, w)
	}
	return q, nil
}

func (d decoder) window(i int, r rawWindow) (Window, error) {
	at := fmt.Sprintf("quiet_hours.windows.%d", i)
	var w Window
	for _, name := range r.Days {
		day, ok := weekdays[name]
		if !ok {
			return w, d.bad(at+".days", "quiet_hours", "mon, tue, wed, thu, fri, sat or sun")
		}
		w.Days = append(w.Days, day)
	}
	var err error
	if w.From, err = parseClock(r.From); err != nil {
		return w, d.bad(at+".from", "quiet_hours", "a 24-hour time such as 22:00")
	}
	if w.To, err = parseClock(r.To); err != nil {
		return w, d.bad(at+".to", "quiet_hours", "a 24-hour time such as 07:00")
	}
	return w, nil
}

func parseClock(s string) (clock, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, err
	}
	return clock(t.Hour()*60 + t.Minute()), nil
}
