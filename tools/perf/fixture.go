package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// levelsConfig is a ladder over the three network channel types, with secret references and no
// values: `levels` reads the config and the state directory but never the secrets file.
const levelsConfig = `version: 1
channels:
  phone: { type: ntfy, server: "https://ntfy.example.com", topic: { secret: ntfy-topic } }
  pager: { type: pagerduty, routing_key: { secret: pd-critical } }
  journal: { type: log, path: /dev/null }
levels:
  - { name: note, use_when: "FYI only", ask: optional, send: [ { channel: journal, interruption: passive }, { channel: phone, interruption: passive } ] }
  - { name: heads-up, use_when: "look soon", send: [ { channel: phone, interruption: active } ] }
  - { name: page, use_when: "production is down", send: [ { channel: phone, interruption: time-sensitive }, { channel: pager, interruption: critical } ] }
`

// levelsFixture writes the config the levels scenario reads, under dir/config/yo-human.
func levelsFixture(dir string) error {
	cfg := filepath.Join(dir, "config", "yo-human")
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cfg, "config.yaml"), []byte(levelsConfig), 0o600)
}

// heldConfig is the levels ladder plus a level that holds in quiet hours, inside a window that is
// never over (a window whose end equals its start is the whole day) and on PagerDuty, which cannot
// schedule: its sends wait in the queue.
const heldConfig = levelsConfig + `  - { name: night, use_when: "can wait until morning", quiet_hours: hold, send: [ { channel: pager, interruption: active } ] }
quiet_hours:
  timezone: UTC
  windows: [ { days: [mon, tue, wed, thu, fri, sat, sun], from: "00:00", to: "00:00" } ]
`

// heldSecrets are fake values for the two secrets the config names, in the secrets file's format.
const heldSecrets = "ntfy-topic: FAKE-PERF-TOPIC\npd-critical: FAKE-PERF-ROUTING\n"

// perfKey is the dedup key of the incident the suppressed scenario repeats.
const perfKey = "perf-incident"

// heldFixture writes the held config and its fake secrets under dir/config/yo-human.
func heldFixture(dir string) error {
	cfg := filepath.Join(dir, "config", "yo-human")
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(cfg, "config.yaml"), []byte(heldConfig), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cfg, "secrets.yaml"), []byte(heldSecrets), 0o600)
}

// suppressedFixture is heldFixture plus an open incident at a louder level than the note the
// scenario sends, last sent just now, that reached a human: "phone" delivered. Without a reached
// entry the incident counts as stranded (no human was told, nothing pending), and its repeat is
// sent and queues (exit 75) instead of being suppressed.
func suppressedFixture(dir string) error {
	if err := heldFixture(dir); err != nil {
		return err
	}
	state := filepath.Join(dir, "state", "yo-human")
	if err := os.MkdirAll(state, 0o700); err != nil {
		return err
	}
	now := time.Now().UTC()
	// The store keys an incident by project and dedup key (core.IncidentKey); the scenario runs with
	// no YO_HUMAN_PROJECT, so its project is "unknown".
	incs := map[string]map[string]any{"unknown\x00" + perfKey: {"key": perfKey, "project": "unknown", "generation": 1, "level": "page", "rank": 2,
		"open": true, "opened": now, "last_sent": now, "updated": now,
		"reached": []map[string]any{{"instance": "phone", "delivered": []string{"FAKE-FP"}}}}}
	b, _ := json.Marshal(incs) // a map of strings, numbers and times always encodes
	return os.WriteFile(filepath.Join(state, "incidents.json"), b, 0o600)
}
