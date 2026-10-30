package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
)

// fingerprintOf identifies where an instance sends: the sha256 of its type, its settings in canonical
// JSON (a secret field holds only the secret's name) and nothing else. No secret value is in a
// config, so none is in the fingerprint; rotating a value changes nothing, naming another secret
// or pointing at another server does.
func fingerprintOf(ci config.ChannelInstance) string {
	b, _ := json.Marshal(struct {
		Type     string         `json:"type"`
		Settings map[string]any `json:"settings"`
	}{ci.Type, ci.Settings}) // settings come from YAML or JSON, so they always encode; maps encode in key order
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// fingerprint is the fingerprint of a configured instance, and empty for a name the config does not
// have.
func (p *Pipeline) fingerprint(name string) string {
	ci, ok := p.d.Config.Channels[name]
	if !ok {
		return ""
	}
	return fingerprintOf(ci)
}

// IncidentKey is the store's key for a project's incident: an agent in one project cannot see,
// suppress or resolve another project's incident by its dedup key. A key and a project never hold
// a control character (the cli refuses or flattens them), so the NUL between them is unambiguous.
// A message with no project (core's own tests) keeps its bare key; the cli always sets one.
func IncidentKey(project, key string) string {
	if project == "" {
		return key
	}
	return project + "\x00" + key
}

// splitIncidentKey is IncidentKey undone: the project and the caller's key.
func splitIncidentKey(id string) (project, key string) {
	if p, k, ok := strings.Cut(id, "\x00"); ok {
		return p, k
	}
	return "", id
}

// incidentID is the store's key for the incident m belongs to.
func incidentID(m channel.Message) string { return IncidentKey(m.Project, m.DedupKey) }

// maxProviderKey is the longest incident key a provider takes (PagerDuty's dedup_key limit).
const maxProviderKey = 255

// ProviderKey is the incident key a provider sees, so one project cannot fold its page into
// another's alert, or resolve it: <len(project)>:<project>/<key>, the byte length making it
// injective (project "a/b" key "c" is 3:a/b/c, project "a" key "b/c" is 1:a/b/c). A pair longer
// than maxProviderKey bytes becomes "h:" and the sha256 of that full form, which no short form (it
// starts with a digit) can equal. No key, or no project (core's own tests), is left as is.
func ProviderKey(project, key string) string {
	if project == "" || key == "" {
		return key
	}
	full := strconv.Itoa(len(project)) + ":" + project + "/" + key
	if len(full) <= maxProviderKey {
		return full
	}
	sum := sha256.Sum256([]byte(full))
	return "h:" + hex.EncodeToString(sum[:])
}
