package cli

// starterConfig is what `config init` writes. It is a starting point: the human edits the
// channels and the ladder. It holds secret names only, never values. %s is the log file path, as
// a quoted YAML string.
const starterConfig = `# yo-human starter config. A starting point: edit the channels and the levels to suit you.
# Run "yo-human config check" after any change. "yo-human config schema" prints every setting.
#
# No secret value belongs in this file. A secret field names a secret ({ secret: <name> }); the
# human stores the value, in their own terminal, with:
#   op read "op://<vault>/<item>/<field>" | yo-human secret set <name>
version: 1

channels:
  phone:                      # ntfy: set server to your own ntfy server
    type: ntfy
    server: https://ntfy.example.com
    topic: { secret: ntfy-topic }
  pager:                      # PagerDuty: one instance per service (urgency is set per service)
    type: pagerduty
    routing_key: { secret: pd-critical }
  journal:                    # a local record of every message; never counts as reaching you
    type: log
    path: %s

# Quietest first. Agents choose a level by its use_when, so write each one for an agent to read.
levels:
  - name: note
    use_when: "FYI. Nothing is wrong and nothing is needed; the human reads it when they next look."
    ask: optional
    send:
      - { channel: journal, interruption: passive }
      - { channel: phone, interruption: passive }
  - name: heads-up
    use_when: "The human should look soon, within the hour, but nothing is on fire."
    send:
      - { channel: journal, interruption: active }
      - { channel: phone, interruption: active }
  - name: page
    use_when: "Production is impaired, or something cannot wait for the human to come back. Wakes them."
    send:
      - { channel: journal, interruption: critical }
      - { channel: phone, interruption: time-sensitive }
      - { channel: pager, interruption: critical }
`
