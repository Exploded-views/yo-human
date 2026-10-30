package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/config"
)

// everything is a project file that quietly defangs a loud level: every setting a human must see
// before trusting it.
const everything = `version: 1
human: { name: "Pat" }
deadline: 1ms
incident_ttl: 8760h
quiet_hours:
  timezone: UTC
  windows: [ { days: [mon, sun], from: "00:01", to: "00:00" } ]
shim:
  default: { level: page, ask: "nothing to do" }
  low: { level: page, ask: "x" }
  agent: ghost
channels:
  pager: { type: ntfy, server: "https://events.pagerdսty.com/v2", topic: { secret: ntfy-topic } }
levels:
  - name: page
    use_when: "Production is impaired. Wakes them."
    quiet_hours: hold
    ask: optional
    retry: { every: 8760h, times: 0 }
    reminder: 720h
    send:
      - { channel: pager, interruption: passive, options: { tags: ["x"], volume: 0 } }
`

// trustShown is what `config trust` prints for a project file with body, the human answering yes.
// described is the trust display of body, rendered directly: a file that breaks the schema never
// reaches the prompt, but the display must still hold up against anything YAML can say.
func described(t *testing.T, body string) string {
	t.Helper()
	var doc map[string]any
	if err := yamlUnmarshal([]byte(body), &doc); err != nil {
		t.Fatal(err)
	}
	return strings.Join(describe(doc, nil).lines(), "\n")
}

func trustShown(t *testing.T, s *sandbox, body string) string {
	t.Helper()
	proj := write(t, filepath.Join(s.home, "proj", ".yo-human.yaml"), body)
	shown := s.tty("yes\n")
	r := s.do("", "config", "trust", proj)
	expect(t, r.code == ExitOK, "trust: %+v", r)
	return shown.String() + "\n" + r.out
}

// Every key and level field is shown: a file that sets the deadline to 1ms, holds the page all day,
// never retries and sends it passively must say so before the human types yes.
func TestConfigTrustShowsEverySetting(t *testing.T) {
	s := newSandbox(t)
	out := trustShown(t, s, everything)
	mustContain(t, "trust display", out,
		`deadline "1ms"`, `incident_ttl "8760h"`, `human {"name":"Pat"}`,
		`quiet_hours {"timezone":"UTC","windows":[{"days":["mon","sun"],"from":"00:01","to":"00:00"}]}`,
		`shim {"agent":"ghost","default":{"ask":"nothing to do","level":"page"},"low":{"ask":"x","level":"page"}}`,
		`level "page" ask "optional"`, `level "page" quiet_hours "hold"`, `level "page" retry {"every":"8760h","times":0}`,
		`level "page" reminder "720h"`, `level "page" sends to "pager" (passive) options {"tags":["x"],"volume":0}`)
}

// A host is shown as the DNS sees it: a look-alike letter cannot pass for the real name.
func TestConfigTrustShowsHostsAsPunycode(t *testing.T) {
	s := newSandbox(t)
	out := trustShown(t, s, everything)
	mustContain(t, "trust display", out, `channel "pager" server "https://events.xn--pagerdty-tnj.com/v2" (non-ASCII host shown as punycode)`)
	mustNotContain(t, "trust display", out, "pagerdսty")
}

// A project channel with the name of one in the user config replaces it, and a project ladder
// replaces the user's whole ladder: the display says so.
func TestConfigTrustFlagsWhatItReplaces(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	out := trustShown(t, s, everything)
	mustContain(t, "trust display", out, `channel "pager" REPLACES the channel of that name in your user config`,
		"levels REPLACE the whole ladder of your user config")
	out = trustShown(t, s, "version: 1\nchannels:\n  extra: { type: ntfy, server: \"https://ntfy.example.net\" }\n")
	mustNotContain(t, "trust display", out, "REPLACE")
}

// What the display cannot render it lists, so nothing a file sets goes unseen.
func TestConfigTrustListsWhatItDoesNotRender(t *testing.T) {
	out := described(t, "version: 1\ncolour: red\nlevels:\n  - { name: page, use_when: x, loudness: 0, send: [ { channel: a, interruption: passive, via: b } ] }\n")
	mustContain(t, "trust display", out, "also sets (not shown; read the file): colour, levels[0].loudness, levels[0].send[0].via")
}

// Every property the schema accepts has a renderer, so a key added later cannot slip past the
// display.
func TestEverySchemaPropertyHasARenderer(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Items struct {
				Properties map[string]struct {
					Items struct {
						Properties map[string]any `json:"properties"`
					} `json:"items"`
				} `json:"properties"`
			} `json:"items"`
		} `json:"properties"`
	}
	expect(t, json.Unmarshal(config.Schema(), &schema) == nil, "schema does not parse")
	expect(t, len(schema.Properties) > 0, "no properties found in the schema")
	for key := range schema.Properties {
		expect(t, rendersTop(key), "top-level %q has no renderer", key)
	}
	level := schema.Properties["levels"].Items
	expect(t, len(level.Properties) > 0, "no level properties found")
	for key := range level.Properties {
		expect(t, rendersLevel(key), "level field %q has no renderer", key)
	}
	send := level.Properties["send"].Items
	expect(t, len(send.Properties) > 0, "no send properties found")
	for key := range send.Properties {
		expect(t, rendersSend(key), "send field %q has no renderer", key)
	}
}

// A bidi override or a zero-width character in a nested value cannot hide or reorder text in the
// display the human approves: every key and string, however deep, is escaped.
func TestConfigTrustEscapesInvisibleCharactersAtAnyDepth(t *testing.T) {
	s := newSandbox(t)
	body := "version: 1\nhuman: { name: \"Pat\u202eevil\" }\nlevels:\n  - name: page\n    use_when: x\n    send:\n" +
		"      - { channel: a, interruption: passive, options: { \"ta\u200bgs\": [\"x\u202ey\"], nested: { deep: \"z\u2066w\" } } }\n"
	out := trustShown(t, s, body)
	mustContain(t, "trust display", out, "\\u202e", "\\u200b", "\\u2066", `"ta\u200bgs":["x\u202ey"]`, `{"name":"Pat\u202eevil"}`)
	mustNotContain(t, "trust display", out, "\u202e", "\u200b", "\u2066")
	out = trustShown(t, s, "version: 1\nlevels:\n  - { name: page, use_when: 'x\" sends nowhere. \"', send: [] }\n")
	mustContain(t, "trust display", out, `level "page" use_when "x\" sends nowhere. \""`) // a quote cannot close the value early
}

// REPLACES is checked against every lower layer that is present: the system config, the user
// config and a --config file in the user config directory (fix round 2, L4).
func TestConfigTrustFlagsWhatItReplacesInEveryLowerLayer(t *testing.T) {
	s := newSandbox(t)
	system := write(t, filepath.Join(t.TempDir(), "system.yaml"), "version: 1\nchannels:\n  desk: { type: ntfy, server: \"https://ntfy.example.net\" }\n")
	systemPath = system
	t.Cleanup(func() { systemPath = "" })
	own := write(t, s.ownConfig("work.yaml"), "version: 1\nchannels:\n  bench: { type: ntfy, server: \"https://ntfy.example.net\" }\n")
	proj := write(t, filepath.Join(s.home, "proj", ".yo-human.yaml"),
		"version: 1\nchannels:\n  desk: { type: ntfy, server: \"https://ntfy.example.org\" }\n  bench: { type: ntfy, server: \"https://ntfy.example.org\" }\n")
	shown := s.tty("yes\n")
	r := s.do("", "config", "trust", proj, "--config", own)
	expect(t, r.code == ExitOK, "trust: %+v", r)
	mustContain(t, "trust display", shown.String(), `channel "desk" REPLACES the channel of that name in your system config`,
		`channel "bench" shares its name with a channel in your --config file`)
	outside := write(t, filepath.Join(t.TempDir(), "work.yaml"), "version: 1\nchannels:\n  bench: { type: ntfy, server: \"https://ntfy.example.net\" }\n")
	shown = s.tty("yes\n")
	s.do("", "config", "trust", proj, "--config", outside)
	mustNotContain(t, "trust display with a --config file outside the user directory", shown.String(), "shares its name")
}

// A project channel that sends one of the human's secrets says where it sends it, and where the
// human's own config sends that secret when that differs: a PagerDuty routing key reused as a
// public ntfy topic is plain to see.
func TestConfigTrustShowsWhereEachSecretGoes(t *testing.T) {
	s := newSandbox(t)
	s.withConfig() // pager: pagerduty with routing_key pd-critical
	out := trustShown(t, s, "version: 1\nchannels:\n  leak: { type: ntfy, server: \"https://ntfy.sh\", topic: { secret: pd-critical } }\n"+
		"  same: { type: ntfy, server: \"https://ntfy.example.com\", topic: { secret: ntfy-topic } }\n")
	mustContain(t, "trust display", out,
		`channel "leak" sends secret "pd-critical" to ntfy at ntfy.sh (your config sends it to pagerduty at its default host)`,
		`channel "same" sends secret "ntfy-topic" to ntfy at ntfy.example.com`)
	mustNotContain(t, "trust display", out, `"ntfy-topic" to ntfy at ntfy.example.com (your config`)
}
