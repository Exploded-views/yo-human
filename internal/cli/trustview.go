package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Exploded-views/yo-human/internal/problem"
	"github.com/Exploded-views/yo-human/internal/secrets"
)

// The keys the trust display renders. Anything else a file sets is listed under "also sets", so
// the display fails closed; a test checks that every schema property is here.
var (
	topKeys   = []string{"version", "human", "deadline", "incident_ttl", "quiet_hours", "shim", "channels", "levels"}
	levelKeys = []string{"name", "use_when", "ask", "quiet_hours", "retry", "reminder", "send"}
	sendKeys  = []string{"channel", "interruption", "options"}
)

// rendersTop, rendersLevel and rendersSend say whether the trust display renders a key.
func rendersTop(key string) bool   { return slices.Contains(topKeys, key) }
func rendersLevel(key string) bool { return slices.Contains(levelKeys, key) }
func rendersSend(key string) bool  { return slices.Contains(sendKeys, key) }

// trustView is what a project file would do, with nothing secret in it.
type trustView struct {
	Settings []settingView `json:"settings"`
	Channels []channelView `json:"channels"`
	Levels   []levelView   `json:"levels"`
	Replaces []string      `json:"replaces"`
	Secrets  []string      `json:"secrets"`
	AlsoSets []string      `json:"also_sets"`
}

type settingView struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type channelView struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Settings map[string]string `json:"settings"`
}

type levelView struct {
	Name    string            `json:"name"`
	UseWhen string            `json:"use_when"`
	Fields  map[string]string `json:"fields"`
	Sends   []sendView        `json:"sends"`
}

type sendView struct {
	Channel      string `json:"channel"`
	Interruption string `json:"interruption"`
	Options      string `json:"options,omitempty"`
}

// esc is the one escaper of everything the trust display shows from a file: a quote and a
// backslash are escaped, and every rune that is not printable, or is a format character (bidi
// overrides and isolates, zero-width spaces and joiners), becomes \uXXXX, so nothing can hide or
// reorder the text the human approves. Bytes that are not UTF-8 become \ufffd.
func esc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case strconv.IsPrint(r) && !unicode.Is(unicode.Cf, r) && r != utf8.RuneError:
			b.WriteRune(r)
		case r > 0xffff:
			fmt.Fprintf(&b, `\U%08x`, r)
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
	}
	return b.String()
}

// value renders any setting as compact JSON, walked here so that every key and every string,
// however deep, goes through esc; keys are sorted.
func value(v any) string {
	switch t := v.(type) {
	case string:
		return `"` + esc(t) + `"`
	case map[string]any:
		return object(t)
	case map[any]any:
		m := make(map[string]any, len(t))
		for k, x := range t {
			m[fmt.Sprint(k)] = x
		}
		return object(m)
	case []any:
		parts := make([]string, len(t))
		for i, x := range t {
			parts[i] = value(x)
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	if tm, ok := v.(time.Time); ok { // YAML reads an unquoted date as a time
		return `"` + esc(tm.Format(time.RFC3339Nano)) + `"`
	}
	b, err := json.Marshal(v) // a number, a bool or null
	if err != nil {
		return "<a value that cannot be shown>"
	}
	return esc(string(b))
}

func object(m map[string]any) string {
	parts := make([]string, 0, len(m))
	for _, k := range sortedNames(m) {
		parts = append(parts, `"`+esc(k)+`":`+value(m[k]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

const literalNote = "holds a literal value (not shown; config check refuses it)"

// describe reads everything a project file sets that a human must see before trusting it. A value
// in a secret field is never shown; settings of a channel type this binary does not know are not
// shown. lower are the other config layers present, to flag what the project file replaces and
// where the human's own config sends each secret the project file uses.
func describe(doc map[string]any, lower []lowerLayer) trustView {
	v := trustView{Settings: []settingView{}, Channels: []channelView{}, Levels: []levelView{}, Replaces: []string{}, Secrets: []string{}, AlsoSets: []string{}}
	for _, key := range sortedNames(doc) {
		switch {
		case !rendersTop(key):
			v.AlsoSets = append(v.AlsoSets, esc(key))
		case key != "channels" && key != "levels":
			v.Settings = append(v.Settings, settingView{key, value(doc[key])})
		}
	}
	chans, _ := doc["channels"].(map[string]any)
	for _, name := range sortedNames(chans) {
		v.Channels = append(v.Channels, describeChannel(name, chans[name]))
	}
	v.Replaces = replaces(doc, lower)
	v.Secrets = secretRoutes(chans, lower)
	levels, _ := doc["levels"].([]any)
	for i, raw := range levels {
		v.Levels = append(v.Levels, v.describeLevel(i, raw))
	}
	return v
}

func describeChannel(name string, raw any) channelView {
	settings, _ := raw.(map[string]any)
	typ, _ := settings["type"].(string)
	cv := channelView{Name: esc(name), Type: esc(typ), Settings: map[string]string{}}
	t, known := channelTypes()[typ]
	if !known {
		cv.Settings["settings"] = "are not shown (unknown channel type)"
		return cv
	}
	for _, key := range sortedNames(settings) {
		if key != "type" {
			cv.Settings[esc(key)] = shown(key, settings[key], slices.Contains(t.SecretFields, key))
		}
	}
	return cv
}

// describeLevel renders one level, noting on v any field it does not render.
func (v *trustView) describeLevel(i int, raw any) levelView {
	l, _ := raw.(map[string]any)
	lv := levelView{Fields: map[string]string{}, Sends: []sendView{}}
	name, _ := l["name"].(string)
	useWhen, _ := l["use_when"].(string)
	lv.Name, lv.UseWhen = esc(name), esc(useWhen)
	for _, key := range sortedNames(l) {
		switch {
		case !rendersLevel(key):
			v.AlsoSets = append(v.AlsoSets, fmt.Sprintf("levels[%d].%s", i, esc(key)))
		case key != "name" && key != "use_when" && key != "send":
			lv.Fields[key] = value(l[key])
		}
	}
	sends, _ := l["send"].([]any)
	for j, rs := range sends {
		s, _ := rs.(map[string]any)
		ch, _ := s["channel"].(string)
		ir, _ := s["interruption"].(string)
		sv := sendView{Channel: esc(ch), Interruption: esc(ir)}
		if opts, ok := s["options"]; ok {
			sv.Options = value(opts)
		}
		for _, key := range sortedNames(s) {
			if !rendersSend(key) {
				v.AlsoSets = append(v.AlsoSets, fmt.Sprintf("levels[%d].send[%d].%s", i, j, esc(key)))
			}
		}
		lv.Sends = append(lv.Sends, sv)
	}
	return lv
}

// shown renders one channel setting for the human. A secret field shows the referenced name or a
// note that it holds a literal; anything else is shown by its text.
func shown(key string, v any, secret bool) string {
	if secret {
		ref, _ := v.(map[string]any)
		name, _ := ref["secret"].(string)
		if len(ref) == 1 && secrets.ValidName(name) {
			return "uses secret " + strconv.Quote(name)
		}
		return literalNote
	}
	s, isStr := v.(string)
	if !isStr {
		return value(v)
	}
	return shownText(key, s)
}

// shownText quotes a path for the shell, shows a URL as scheme, host and path only, and quotes
// anything else.
func shownText(key, s string) string {
	switch {
	case key == "path" || key == "file":
		return problem.ShellQuote(s)
	case key == "server" || strings.HasSuffix(key, "url") || strings.Contains(s, "://"):
		return endpoint(s)
	}
	return strconv.Quote(s)
}

// endpoint shows scheme, host and path only: a userinfo, query or fragment may carry a secret. The
// host is shown as the DNS sees it, in Punycode. A value with no scheme or host is not a URL.
func endpoint(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "<not a valid URL>"
	}
	host, puny := asciiHost(u.Host)
	out := strconv.Quote(u.Scheme + "://" + host + u.EscapedPath())
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		out += " (credentials, query or fragment hidden)"
	}
	if puny {
		out += " (non-ASCII host shown as punycode)"
	}
	return out
}

// lines renders the view for the terminal.
func (v trustView) lines() []string {
	var out []string
	for _, s := range v.Settings {
		out = append(out, s.Key+" "+s.Value)
	}
	for _, c := range v.Channels {
		q := `"` + c.Name + `"`
		out = append(out, "channel "+q+` has type "`+c.Type+`"`)
		for _, key := range sortedNames(c.Settings) {
			out = append(out, "channel "+q+" "+key+" "+c.Settings[key])
		}
	}
	out = append(out, v.Replaces...)
	out = append(out, v.Secrets...)
	for _, l := range v.Levels {
		out = append(out, l.lines()...)
	}
	if len(v.AlsoSets) > 0 {
		out = append(out, "also sets (not shown; read the file): "+strings.Join(v.AlsoSets, ", "))
	}
	return out
}

func (l levelView) lines() []string {
	q := `level "` + l.Name + `"`
	var out []string
	if l.UseWhen != "" {
		out = append(out, q+` use_when "`+l.UseWhen+`"`)
	}
	for _, key := range sortedNames(l.Fields) {
		out = append(out, q+" "+key+" "+l.Fields[key])
	}
	route := "nowhere"
	if len(l.Sends) > 0 {
		parts := make([]string, len(l.Sends))
		for i, s := range l.Sends {
			parts[i] = `"` + s.Channel + `" (` + s.Interruption + ")"
			if s.Options != "" {
				parts[i] += " options " + s.Options
			}
		}
		route = "to " + strings.Join(parts, ", ")
	}
	return append(out, q+" sends "+route)
}
