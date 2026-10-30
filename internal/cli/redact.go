package cli

import (
	"sort"
	"strings"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
	"github.com/Exploded-views/yo-human/internal/secrets"
)

// redacted replaces a stored secret value found in a message.
const redacted = "REDACTED"

// minRedact is the shortest value that is redacted: a shorter one would turn ordinary text into
// noise, and no provider's credential is that short.
const minRedact = 4

// secretValues reads the secrets file once per run.
func (e *env) secretValues() (map[string]string, error) {
	if e.secretVals != nil {
		return e.secretVals, nil
	}
	vals, err := secrets.Load(e.secretsFile())
	if err == nil {
		e.secretVals = vals
	}
	return vals, err
}

// messageFields are the text fields of m that reach a channel.
func messageFields(m *channel.Message) []*string {
	fields := []*string{&m.Title, &m.Ask, &m.Body, &m.Link, &m.DedupKey, &m.Project, &m.Agent}
	for i := range m.Actions {
		fields = append(fields, &m.Actions[i].Label, &m.Actions[i].URL)
	}
	return fields
}

// redact replaces every stored secret value in the fields, longest first, and reports whether it
// replaced any. It warns of nothing itself: see warnRedacted.
func (e *env) redact(fields ...*string) (bool, error) {
	vals, err := e.secretValues()
	if err != nil {
		return false, err
	}
	names := make([]string, 0, len(vals))
	for name := range vals {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return len(vals[names[i]]) > len(vals[names[j]]) || (len(vals[names[i]]) == len(vals[names[j]]) && names[i] < names[j])
	})
	found := false
	for _, name := range names {
		if v := vals[name]; len(v) >= minRedact && replaceIn(fields, v) {
			found = true
		}
	}
	return found, nil
}

// warnRedacted warns that a message held a secret value, once the call has sent or queued
// something. It never names the secret, and a call that sent nothing (a noop resolve, a suppressed
// repeat) says nothing: the warning must not tell a caller which guess matched which secret.
func (e *env) warnRedacted(found bool, res core.Result) {
	if !found || res.Status == "" || res.Status == core.StatusNoop || res.Status == core.StatusSuppressed {
		return
	}
	e.warn([]*problem.Problem{problem.Usagef("warning: the message held the value of a secret; it was replaced with %s before it was queued",
		"a message that says where a secret is, never its value",
		"never put a secret in a message; tell the human a secret may be exposed so they can check and rotate it", redacted)})
}

// replaceIn replaces v in every field and reports whether any held it.
func replaceIn(fields []*string, v string) bool {
	hit := false
	for _, f := range fields {
		if strings.Contains(*f, v) {
			*f, hit = strings.ReplaceAll(*f, v, redacted), true
		}
	}
	return hit
}
