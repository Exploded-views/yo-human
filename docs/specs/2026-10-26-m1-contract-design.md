# yo-human M1: the v1 contract

Status: draft for the owner's sign-off, 2026-10-26. Issue: #27. Builds on the [design](2026-10-26-yo-human-design.md), the [constitution](../../constitution.md) and the owner decisions #2–#10. After sign-off, M1 gets a writing-plans plan from this document.

Each point is marked as either the **owner's** decision (made in the 2026-10-26 conversation) or the **builder's call** (made by Claude when the owner handed it over; the owner checks these while reading).

## Intent

yo-human is a **notification tool, not a decision-maker** (owner). Agents work inside a larger operating model, and that model and the agents' rules decide *whether* the human is needed and *how urgently*. Agents don't act on production unplanned without the human, so when the process says "human needed", the work waits. It can't wait until the human happens to be back at the keyboard. yo-human's job is to reach the human **politely and according to the human's wishes**, through one tool, so nobody needs a bespoke script per situation (owner).

Real cases behind it (owner):
- production impaired for reasons within our control
- a vendor security breach caught on a status page before the vendor announced it, needing every key rotated at once
- agents watching the human's email or other inboxes and passing on something that needs their eyes

Intensity runs from an FYI up to "get to your laptop or phone now". The calling agent chooses the intensity, never yo-human.

## Goals, non-goals, anti-goals

- **Goal:** one CLI call delivers a message at the intensity the agent chose, over whatever mix of channels the human configured, within a bounded time, and never loses it.
- **Non-goal:** deciding whether something deserves to reach the human. That lives in the agents' rules and operating model (owner).
- **Non-goal:** hard-coding any level, channel or provider. Everything is configuration (owner: "all generalized and configurable, ideally through simple YAML or JSON").
- **Anti-goals** (from the design): noise, paging twice for one thing, silent loss, blocking the agent, a security hole.

## Mix and match is the rule

People will mix and match notification providers (owner). PagerDuty, ntfy and Bark are only the **first channel types**, plus a local `log` type. The core never names a provider.
- Each channel type **declares its capabilities** from a closed list (below).
- Every behaviour that differs by provider comes from those capabilities, never from the type's name: rendering, intensity, repeats, resolve, deferred delivery.
- A level can send to any mix of channel instances.
- `config check` warns when a level asks for something its channels can't do, for example `critical` on a channel without critical alerts.

## Configuration (owner: YAML or JSON)

The user-level file is `$XDG_CONFIG_HOME/yo-human/config.yaml` (usually `$HOME/.config/yo-human/config.yaml`; **builder's call**), and other layers can sit closer to the caller (see Config layers below).
- It is parsed as YAML, so plain JSON also works.
- It is validated against a JSON Schema the binary embeds and prints with `yo-human config schema`.
- An agent editing the config conversationally gets the same AI-oriented errors as on any other call.

Shape (field names are the **builder's call**; the values are illustrative only, since the owner chooses every name):

```yaml
version: 1
human: { name: "<the human's name>" }
deadline: 1s                         # total time a call may take; default 1s (owner, Q4)
incident_ttl: 24h                    # an unresolved incident closes itself after this (builder's call)

channels:                            # named instances of channel types; mix and match
  <instance-name>:
    type: ntfy | bark | pagerduty | log
    secret: <secret-name>            # a name from the secrets file, never a value
    <type-specific settings>         # validated by that type's schema

levels:                              # ordered, quietest first; names are the owner's
  - name: <level-name>
    use_when: "<guidance for agents choosing this level>"   # required
    ask: required | optional         # default required: every page says what it needs
    quiet_hours: hold | send         # default send
    retry: { every: 60s, times: 5 }  # what a "queued" result tells the agent to do
    reminder: 30m                    # optional; re-send an open, unchanged repeat after this long
    send:
      - channel: <instance-name>
        interruption: passive | active | time-sensitive | critical
        options: { <type-specific per-send settings> }

quiet_hours:
  timezone: <IANA name>
  windows: [ { days: [mon, tue, wed, thu, fri, sat, sun], from: "22:00", to: "07:00" } ]
```

### Config layers: closest wins (owner)

Config can sit at several levels, and the closest to the caller wins (owner). Lookup, closest first:
1. `--config <file>` or `YO_HUMAN_CONFIG`.
2. **Project:** `.yo-human.yaml` or `.yo-human.json` in the working directory or its nearest ancestor that has one.
3. **User:** `$XDG_CONFIG_HOME/yo-human/config.yaml`.
4. **System:** `/etc/yo-human/config.yaml`.

Merge rules (**builder's call**):
- Single settings (`deadline`, `incident_ttl`, `quiet_hours`, `human`, `shim`) come from the closest file that sets them.
- `channels` merge by instance name; a closer file adds instances or replaces one of the same name.
- `levels` is replaced whole by the closest file that defines it. Mixing rungs from two files would scramble the order agents rely on.
- `config check` prints the merged result and, for each setting, the file it came from.

Safety (owner):
- **Secrets live only in the user layer's secrets file.** No config file at any layer holds a secret value.
- **A project file counts only after the human trusts it.** `yo-human config trust [<file>]` records the file's path and sha256 in the user state directory. A project file that is untrusted, or whose hash has changed since it was trusted, is ignored, and every command says so on stderr with the `trust` command to run. This stops a cloned or compromised repo from quietly turning a loud level into a log line or redirecting pages.

`interruption` is the one intensity vocabulary shared by every channel type (**builder's call**). Each type maps it to its own controls, for example:
- ntfy: priorities 2 / 3 / 4 / 5
- Bark: `passive` / `active` / `timeSensitive` / `critical`
- PagerDuty: severities `info` / `warning` / `error` / `critical`

Anything only one provider has (Bark's `call`, `volume` and `sound`; ntfy's tags; the PagerDuty routing key per service) goes in `options` and is validated by that type.

**Credentials never sit in the config** (**builder's call**, following the owner's secrets decision and constitution VIII):
- Each channel type's schema marks which of its fields are secrets: a PagerDuty routing key, an ntfy access token, an ntfy topic (it works as a password), a Bark device key.
- Those fields accept only secret names (`routing_key: { secret: pd-critical }`), never values. An instance may reference several secrets, or none (an open ntfy topic).
- `config check` fails if a secret field holds a literal.

## Secrets (owner: local file, option 2)

- **Where:** `$XDG_CONFIG_HOME/yo-human/secrets.yaml`, with the file at mode 0600 and its directory at 0700 (**builder's call** on paths). yo-human refuses to run if either is looser: exit 78, with the exact `chmod` to run.
- **How they get in:** `yo-human secret set <name>` reads the value from stdin only (never from an argument), so it is piped from 1Password and never pasted: `op read "op://<vault>/<item>/<field>" | yo-human secret set <name>`. The file is written atomically.
- **Listing:** `yo-human secret list` prints names and byte counts, never values.
- **Never printed:** no output, log or error ever contains a secret value.

## The message (owner: a shared core, option 2)

Agents send the same fields whatever the channels are. Each channel shows as much as its capabilities allow.

| Field | Flag | Rules (limits are the **builder's call**) |
|---|---|---|
| level | `--level <name>` | Required. A name from the config (owner). |
| title | `--title` | Required, ≤ 200 characters. |
| ask | `--ask` | What the human needs to do. Required unless the level says `ask: optional`. |
| body | `--body`, `--body-file <path>` or `--body -` (stdin) | Optional, ≤ 16 KB; channels truncate to their own limits with a visible marker. |
| link | `--link <url>` | Optional, http(s) only. |
| actions | `--action "<label>=<url>"`, repeatable, up to 3 | Buttons where the channel supports them; links in the body otherwise. |
| source | `--project`, `--agent` | Default from `YO_HUMAN_PROJECT` / `YO_HUMAN_AGENT`, else `unknown`. |
| dedup key | `--dedup-key` | Optional, ≤ 255 characters (PagerDuty's limit). Without one, nothing is deduplicated. |

Each message gets an id. **Builder's call:** a ULID, so ids sort by time.

How a channel shows the core fields when it supports them (from the 2026-10-26 docs check):
- **title:** ntfy title, Bark title, PagerDuty summary
- **ask:** Bark subtitle; the first line of the body elsewhere
- **link:** ntfy click, Bark url, PagerDuty links
- **actions:** ntfy actions; links in the body elsewhere
- **source:** ntfy tags, Bark group, PagerDuty source and component
- **dedup key:** PagerDuty `dedup_key`; yo-human's own record elsewhere

## Commands

**Builder's call** on the names; owner decisions on the behaviour.

- `yo-human notify …`: send a message. The flags are in the table above.
- `yo-human resolve --dedup-key <key> [--note <text>]`:
  - **first cancels everything still pending for that incident** (**builder's call**): queued and held sends are removed, and messages a channel has scheduled are cancelled through the channel. Nothing pages the human at 7am about something fixed at 3am.
  - closes the incident with channels that can resolve (PagerDuty)
  - sends a "resolved: …" note to the other channels the incident **actually reached**, with `passive` interruption (or the channel's quietest), ignoring quiet hours, since it doesn't buzz (owner, with the details as the **builder's call**). The incident records each reached channel instance, so it can be addressed later.
  - an unknown or already-resolved key exits 0 with status `noop`
- `yo-human flush`: send whatever is queued or held and now due, within the deadline.
- `yo-human status`: queued, held, dead-lettered and open incidents (by dedup key).
- `yo-human levels`: the configured levels, quietest first, with `use_when`. This is how an agent's skill learns the human's ladder (owner: names from config).
- `yo-human config init`: write a starter config, refusing to overwrite an existing one.
- `yo-human config trust [<file>]`: trust the project-level config file (it is re-checked by sha256 on every call).
- `yo-human config check`: validate the config and secrets, and print capability warnings.
- `yo-human config schema`: print the embedded schema.
- `yo-human channel test <instance>`: send a test message through one channel.
- `yo-human secret set <name>` (stdin) and `yo-human secret list`.
- `yo-human version`.
- `yo-human page …`: the drop-in shim (below).

### The shim for the existing paging script (owner #8)

The existing script's interface, which the shim keeps:
- `<script> [--low] "<summary>" [details] [dedup-key]` triggers. Without a key, the dedup key defaults to `orch-<UTC timestamp, %Y%m%dT%H%M%SZ>`.
- `<script> [--low] --resolve <dedup-key>` resolves.
- The default is critical (the "need human now" service); `--low` is the "CI/CD broken while prod is up" service.

`yo-human page` takes exactly those arguments. A config block maps the two modes onto the human's levels, with the ask the old script implied (**builder's call** on the shape):

```yaml
shim:
  default: { level: <level-name>, ask: "<what the human must do, e.g. look now>" }
  low:     { level: <level-name>, ask: "<…>" }
  agent: orchestrator              # source when YO_HUMAN_AGENT is unset
```

- The summary becomes the title, and details become the body.
- Migration is one line per host: the old script becomes `exec yo-human page "$@"`.
- Output is the normal `RESULT` line, not PagerDuty's JSON. Exit codes are yo-human's; callers that only test for zero or non-zero behave as before, and a queued page (75) is correctly non-zero.
- Without a `shim` block, `yo-human page` exits 78 and names the block to add.

Every command takes `--json`. Every command first sends anything due in the queue, within the same deadline (constitution V), except `secret`, `config` and `version`.

## Delivering a notify (owner: fall back across channels, then queue)

1. **Validate** the message. Any problem exits 64, with an AI-oriented error.
2. **Repeats** (owner: suppress unless louder). If the dedup key belongs to an open incident:
   - at the same or a lower level, the call is recorded and exits 0 with status `suppressed`, unless the level's `reminder` interval has passed since the last send
   - at a higher level, it is sent, and the incident's level is raised
   - **a changed repeat at the same level** (**builder's call**): on channels with `NativeDedup` (PagerDuty), it is sent as an update to the existing alert, which doesn't re-page. Elsewhere it is recorded, the latest content is kept on the incident (`status` shows it), and nothing is sent.
   - **incidents expire** (**builder's call**). An incident nobody resolves closes itself after `incident_ttl` (a top-level config setting, default 24h), so a forgotten key can't silence an alert forever. Expiry sends nothing.
   - **holding the lock** (**builder's call**). The incident check and the record happen under one hold of the incident lock, so two concurrent repeats can't both page.
3. **Quiet hours.** If the level says `quiet_hours: hold` and we're inside a window, each send is deferred to the end of the window:
   - by the channel itself, when it can schedule delivery (ntfy)
   - otherwise by holding it in the queue, so the first `notify` or `flush` after the window delivers it. A cron entry running `yo-human flush` every few minutes is recommended, and the setup docs say so.
   - the result is status `held` (exit 0)
4. **Enqueue first** (constitution V). The message is written to the queue before any network call.
5. **Send to every channel the level lists, in parallel,** within the remaining deadline (**builder's call:** all of them, not the first that works, because a loud level usually wants several at once). The new message goes before any older backlog. Results count **only channels that reach a human** (the `ReachesHuman` capability; the `log` type doesn't), so a log line never makes a failed page look delivered (**builder's call**):
   - every human-reaching channel succeeds: status `delivered` (exit 0)
   - at least one succeeds: status `partial` (exit 0). The human was reached, and the failed channels stay queued for retry.
   - none succeeds, but at least one failure can be retried: status `queued` (exit 75). The output tells the agent exactly what to do, built from the level's `retry` setting.
   - every failure is permanent (a revoked key, a 400): status `failed` (exit 69). The sends go to the dead letter, and the agent must stop and tell the human, not retry (**builder's call**).
   - a channel error that can't be fixed by retrying always goes to the dead letter for that channel and is reported. It is never retried silently.
   - `config check` refuses a level with no human-reaching channel.
6. **Bounded retries** per channel, with backoff recorded in the store. When they run out, the send goes to the dead letter, which `status` shows and which is never deleted.

## Output and exit codes

**Builder's call**, following bitc's documented style.

The first line of stdout is always `RESULT <status> <id>`. The statuses are `delivered`, `partial`, `queued`, `failed`, `held`, `suppressed`, `resolved` and `noop`. Read-only commands (`status`, `levels`, `config …`, `secret list`) print `RESULT ok`. Human-readable lines follow. With `--json`, stdout instead carries a single object:

```json
{"version": 1, "status": "...", "id": "...",
 "channels": [{"channel": "...", "status": "sent|queued|held|dead|skipped", "error": "..."}],
 "next": {"do": "yo-human flush", "after_seconds": 60, "attempts_left": 5, "then": "tell the human in your session"}}
```

| Exit | Meaning | What the agent does |
|---|---|---|
| 0 | Done: delivered, partial, held, suppressed, resolved or noop | Carry on. |
| 64 | Usage: an unknown level, a missing title or ask, a bad flag | Fix the call; the error says how. |
| 75 | Queued: no channel reachable yet | Follow `next` (bounded retries, then tell the human in the session). |
| 69 | Failed: every human-reaching channel rejected the message permanently | Stop and tell the human; retrying can't help. |
| 78 | Config: an invalid config, a missing secret, loose permissions | Stop and tell the human; only they can fix it. |
| 74 | Store I/O failure | Stop and tell the human. |
| 70 | Internal error (a bug) | Stop and tell the human. |

**Errors are written for an AI** (owner) and go to stderr (or into the `--json` object) as `error:`, `valid:` and `fix:` lines. For example:

```
error: unknown level "urgent"
valid: levels, quietest first: <name> (use when …), <name> (use when …)
fix: retry with --level <name>; run "yo-human levels --json" to read them
```

## Store and locking

**Builder's call**: plain files, no database. The binary is `CGO_ENABLED=0`, and files are easy to inspect.

- **State directory:** `$XDG_STATE_HOME/yo-human/` (usually `$HOME/.local/state/yo-human/`), mode 0700.
- **Layout:**
  - `queue/<id>.<channel>.json`: one file per pending send
  - `inflight/`: sends being attempted right now
  - `dead/`: sends that won't be retried
  - `incidents.json`: dedup key → level, status, the channels reached, last sent
  - `log.jsonl`: an append-only record of results, never with secret values
- **Atomic writes:** every write goes to a temp file, is fsynced, then renamed.
- **Claiming a send:** an atomic `rename` from `queue/` to `inflight/<id>.<channel>.<pid>.json` claims it. So two concurrent calls can never send the same message, and no lock is held during network I/O.
  - An inflight file older than twice the deadline whose pid is gone is returned to the queue.
- **Locking:** a short `flock` on `state.lock` guards `incidents.json` reads and writes only.
- **Single write path:** every write goes through `internal/store` (constitution IV).

## The channel interface

**Builder's call**: this is the seam the M1 lanes and M4's plugins build against.

```go
// internal/channel
type Capabilities struct {
    Interruptions map[Interruption]bool // passive, active, time-sensitive, critical
    Markdown      bool
    Link          bool
    MaxActions    int  // 0 = none
    Subtitle      bool // a separate line for the ask
    NativeDedup   bool // the provider updates an existing alert by key
    Resolve       bool // the provider can close an incident
    Schedule      bool // the provider can deliver at a later time, and cancel it
    ReachesHuman  bool // false for log-like types; they never count as delivery
    MaxTitle, MaxBody int
}

type Channel interface {
    Type() string
    Capabilities() Capabilities
    Validate(options map[string]any) error // per-send options from the config
    Send(ctx context.Context, m Message, s SendSpec) (Receipt, error)
    Resolve(ctx context.Context, dedupKey string, note Message) error // only if Capabilities.Resolve
    Cancel(ctx context.Context, r Receipt) error                      // only if Capabilities.Schedule
}
```

- **Errors:** they carry a kind (`Transient`, `Permanent`, `Config`), which the pipeline maps to retry, dead letter or exit 78.
- **What a channel receives:** a `SendSpec` holding the interruption, the delivery time (for `Schedule`), the per-send options, and its resolved secret value.
- **What a channel can't touch:** the store or other channels (constitution IV, enforced by depguard).

## Constraints that bind M1

- **Performance (constitution VII):** `notify` takes at most the deadline. Every path with no network work (validation, suppressed, held without scheduling, the queued write, `status`, `levels`) has a p95 budget in the tens of milliseconds, in `perf-budgets.json`, measured with an empty queue.
- **Free by default:** all v1 channel types work on free tiers (self-hosted ntfy, Bark, PagerDuty Free).
- **Known provider limits** (from the 2026-10-26 docs check; config check and the setup docs cover them):
  - Self-hosted ntfy reaches iOS instantly only with `upstream-base-url` set, and it has no iOS critical alerts.
  - Bark can't update or close a notification.
  - PagerDuty Free's urgency is set per service, so a loud and a quiet PagerDuty send are two channel instances (two services).
- **Out of v1** (owner #2, #9): `ask`/`wait`, question sessions, reading acknowledgements back, MCP, plugins out of process (M4), Pushover (M4).

## Left to the builder

- Go type names beyond the channel interface, the package layout within the constitution's layers, and the exact JSON Schema text.
- Each channel type's full field mapping and its option schema.
- The backoff curve within a level's `retry` setting.
- The default config `yo-human config init` writes. It must be a sensible ladder for a new user, and it is clearly marked as a starting point.
