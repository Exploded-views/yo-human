package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/problem"
	"github.com/Exploded-views/yo-human/internal/secrets"
)

const warningPrefix = "warning: "

// IsWarning reports whether p is advice (a level asks for something its channel cannot do) rather
// than a fault that stops the config from working.
func IsWarning(p *problem.Problem) bool { return strings.HasPrefix(p.What, warningPrefix) }

// Rank is the level's position in the ladder, quietest first.
func (c *Config) Rank(level string) (int, bool) {
	for i, l := range c.Levels {
		if l.Name == level {
			return i, true
		}
	}
	return 0, false
}

// secretRef returns the secret name when v is exactly {secret: <name>}.
func secretRef(v any) (string, bool) {
	m, ok := v.(map[string]any)
	if !ok || len(m) != 1 {
		return "", false
	}
	name, ok := m["secret"].(string)
	return name, ok && secrets.ValidName(name)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func typeNames(reg channel.Registry) string {
	return strings.Join(sortedKeys(reg), ", ")
}

func unknownType(name, typ string, reg channel.Registry) *problem.Problem {
	return problem.Configf("channels.%s has unknown type %q", "one of: "+typeNames(reg),
		"set channels."+name+".type to one of: "+typeNames(reg), name, typ)
}

// notAReference never quotes the value it found: it may be a pasted secret.
func notAReference(name, field string) *problem.Problem {
	return problem.Configf("channels.%s.%s must be a secret reference, not a value", "{ secret: <name> }, where <name> is 1 to 63 lower-case letters, digits and hyphens, starting with a letter or digit, not a run of 16 or more hex digits (that looks like a token), and is stored in the secrets file",
		"tell the human: they store the value with `yo-human secret set <name>` in their own terminal (piped in on stdin), then write { secret: <name> } here, using the name and never the value", name, field)
}

func missingSecret(secret, name, field string) *problem.Problem {
	return problem.Configf("secret %q, used by channels.%s.%s, is not set", "a name listed by `yo-human secret list`",
		`tell the human to run, in their own terminal: op read "op://<vault>/<item>/<field>" | yo-human secret set `+secret, secret, name, field)
}

// Instances turns each configured channel into a channel.Instance, replacing each {secret: name}
// with its value. The values land in Instance.Secrets, keyed by the settings field that named
// them, and the field leaves Settings. A literal where a reference belongs, or a name missing from
// secretValues, is a Config problem that never quotes a value.
func (c *Config) Instances(secretValues map[string]string, reg channel.Registry) (map[string]channel.Instance, error) {
	out := make(map[string]channel.Instance, len(c.Channels))
	for _, name := range sortedKeys(c.Channels) {
		ci := c.Channels[name]
		t, ok := reg[ci.Type]
		if !ok {
			return nil, unknownType(name, ci.Type, reg)
		}
		in := channel.Instance{Name: name, Type: ci.Type, Settings: make(map[string]any, len(ci.Settings)), Secrets: map[string]string{}}
		for k, v := range ci.Settings {
			in.Settings[k] = v
		}
		for _, field := range t.SecretFields {
			v, present := ci.Settings[field]
			if !present {
				continue
			}
			ref, ok := secretRef(v)
			if !ok {
				return nil, notAReference(name, field)
			}
			val, ok := secretValues[ref]
			if !ok {
				return nil, missingSecret(ref, name, field)
			}
			in.Secrets[field] = val
			delete(in.Settings, field)
		}
		out[name] = in
	}
	return out, nil
}

// Check lists everything wrong with c: channel types and settings, secret fields holding values,
// level structure, sends that reference nothing or cannot be done, and the shim. Problems that are
// advice rather than faults satisfy IsWarning. A secret missing from the secrets file is not a
// finding here; Instances reports it.
func Check(c *Config, reg channel.Registry) []*problem.Problem {
	out := checkChannels(c, reg)
	out = append(out, checkLevels(c, reg)...)
	out = append(out, checkShim(c)...)
	return append(out, checkLogPaths(c)...)
}

func checkChannels(c *Config, reg channel.Registry) []*problem.Problem {
	var out []*problem.Problem
	for _, name := range sortedKeys(c.Channels) {
		ci := c.Channels[name]
		t, ok := reg[ci.Type]
		if !ok {
			out = append(out, unknownType(name, ci.Type, reg))
			continue
		}
		settings := make(map[string]any, len(ci.Settings))
		for k, v := range ci.Settings {
			settings[k] = v
		}
		for _, field := range t.SecretFields {
			v, present := settings[field]
			if !present {
				continue
			}
			if _, ok := secretRef(v); !ok {
				out = append(out, notAReference(name, field))
			}
			settings[field] = map[string]any{"secret": "placeholder"} // keep the value out of the schema pass
		}
		out = append(out, checkSettings(name, t, settings)...)
	}
	return out
}

func checkSettings(name string, t channel.Type, settings map[string]any) []*problem.Problem {
	if len(t.SettingsSchema) == 0 {
		return nil
	}
	sch, err := compileNode(t.SettingsSchema)
	if err != nil {
		return []*problem.Problem{problem.Internalf("the %s type's settings schema does not compile", "a valid JSON Schema", "report this as a bug", t.Name)}
	}
	found, err := nodeViolations(sch, settings)
	if err != nil {
		return []*problem.Problem{problem.Configf("channels.%s holds a structure JSON cannot express", "plain text keys and values",
			"simplify channels."+name, name)}
	}
	var out []*problem.Problem
	for _, v := range found {
		out = append(out, problem.Configf("channels.%s.%s", fmt.Sprintf("settings the %s type accepts", t.Name),
			fmt.Sprintf("edit that setting to match the %s type's schema", t.Name), name, v.String()))
	}
	return out
}

func checkLevels(c *Config, reg channel.Registry) []*problem.Problem {
	var out []*problem.Problem
	seen := map[string]bool{}
	for _, l := range c.Levels {
		if seen[l.Name] {
			out = append(out, problem.Configf("duplicate level name %q", "each level name appears once",
				"rename one of the levels so every name is unique", l.Name))
		}
		seen[l.Name] = true
		if l.UseWhen == "" {
			out = append(out, problem.Configf("level %q has no use_when", "a sentence telling agents when to choose this level",
				"add a use_when line to the level", l.Name))
		}
		out = append(out, checkSends(c, reg, l)...)
	}
	return out
}

// checkSends checks one level's sends. The reaches-a-human check is skipped when a send could not
// be resolved, since the missing piece is already reported and may be the one that reaches.
func checkSends(c *Config, reg channel.Registry, l Level) []*problem.Problem {
	var out []*problem.Problem
	reaches, unresolved := false, false
	for _, s := range l.Send {
		ci, ok := c.Channels[s.Channel]
		if !ok {
			unresolved = true
			out = append(out, problem.Configf("level %q sends to unknown channel %q", "a name under channels: "+strings.Join(sortedKeys(c.Channels), ", "),
				"add a channels entry named "+s.Channel+", or send to an existing one", l.Name, s.Channel))
			continue
		}
		t, ok := reg[ci.Type]
		if !ok {
			unresolved = true
			continue
		}
		reaches = reaches || t.Capabilities.ReachesHuman
		if !t.Capabilities.Interruptions[s.Interruption] {
			out = append(out, cannotInterrupt(reg, l, s, ci.Type))
		}
	}
	if !reaches && !unresolved {
		out = append(out, problem.Configf("level %q has no channel that reaches a human", "at least one send to a channel whose type reaches a human (a log does not)",
			"add a send to a channel of a type such as "+typeNames(humanTypes(reg)), l.Name))
	}
	return out
}

func humanTypes(reg channel.Registry) channel.Registry {
	out := channel.Registry{}
	for n, t := range reg {
		if t.Capabilities.ReachesHuman {
			out[n] = t
		}
	}
	return out
}

func cannotInterrupt(reg channel.Registry, l Level, s Send, typ string) *problem.Problem {
	can := channel.Registry{}
	for n, t := range reg {
		if t.Capabilities.Interruptions[s.Interruption] {
			can[n] = t
		}
	}
	p := problem.Configf(warningPrefix+"level %q sends %s to channel %q, whose type %s cannot do %s",
		"passive, active, time-sensitive or critical, as the channel type supports",
		fmt.Sprintf("use a channel of a type that can (%s), or lower the interruption", typeNames(can)),
		l.Name, s.Interruption, s.Channel, typ, s.Interruption)
	return p
}

func checkShim(c *Config) []*problem.Problem {
	if c.Shim == nil {
		return nil
	}
	var out []*problem.Problem
	for _, m := range []struct{ path, level string }{{"shim.default.level", c.Shim.Default.Level}, {"shim.low.level", c.Shim.Low.Level}} {
		if m.level == "" {
			continue
		}
		if _, ok := c.Rank(m.level); !ok {
			var names []string
			for _, l := range c.Levels {
				names = append(names, l.Name)
			}
			out = append(out, problem.Configf("%s names unknown level %q", "a name under levels: "+strings.Join(names, ", "),
				"set "+m.path+" to one of the names under levels", m.path, m.level))
		}
	}
	return out
}
