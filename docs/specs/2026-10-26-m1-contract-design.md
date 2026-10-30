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
- ntfy: priorities 2 / 3 / 4 (no 5; see the amendments)
- Bark: `passive` / `active` / `timeSensitive` / `critical`
- PagerDuty: severities `info` / `warning` / `error` / `critical`

Anything only one provider has (Bark's `call`, `volume` and `sound`; ntfy's tags and markdown) goes in `options` and is validated by that type.

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
- **What a channel receives:** a `SendSpec` holding the interruption, the delivery time (for `Schedule`) and the per-send options. Its credentials reach it at construction, through the resolved instance (`Instance.Secrets`), never per send.
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

## Amendments (2026-10-26)

The owner approved these changes so that the spec matches the shipped behaviour. Each was checked against the code; the one place where the code differs from the owner's wording is noted.

1. **No per-send routing key.** The PagerDuty routing key per service is not a per-send option. Two services are two channel instances, and credentials arrive only at construction (see the channel interface). A PagerDuty send takes no options at all.
2. **Send order.** The new message goes first, then the backlog, in the same call.
3. **Help and usage.** Running `yo-human` with no arguments prints the usage and exits 64. `--help` (like any word that is not a command) exits 64 with the unknown-command error, which lists the commands and points at the no-argument usage. *The code differs from the approved wording here: `--help` does not print the usage itself.*
4. **`levels --json`** has no `send` field. It lists each level's name, `use_when`, `ask` and `quiet_hours`.
5. **`problem.Unavailable` maps to exit 69.**
6. **ntfy has no priority 5.** ntfy maps `passive`, `active` and `time-sensitive` to priorities 2, 3 and 4. It declares no `critical`, and `config check` warns when a level sends `critical` to it. Critical pages go through Bark or PagerDuty.
7. **`--json` output.** stdout always carries exactly one object. Problems that come with a result are in its `errors` array, and warnings are in `warnings`.
8. **`resolve` exits** 75 (with `next`) or 69 when a scheduled send could not be cancelled: 75 when it is queued for another try, 69 when it is dead.
9. **`flush` exits** 69, 75 or 0 based on the entries it attempted: 69 if any went dead, else 75 if any is queued again (with `next`), else 0. Parked entries do not count.
10. **Project-layer config may not define `log` channels.** `config trust` shows every channel, setting, routing and `use_when` before it records the trust.
11. **Queued entries are pinned to a destination fingerprint.** An entry that cannot be sent under the config in force is parked with a reason (shown by `status`), never dead-lettered. A plain `flush` leaves entries queued under another config parked; `flush --config <path>` sends them.
12. **A repeat that reached no human is sent.** A repeat of an incident that reached no human, with nothing pending, is sent rather than suppressed.
13. **A caller's `--config` needs trust (security review, ruling 1).** A `--config` or `YO_HUMAN_CONFIG` file counts as the user's own only when it lives in `$XDG_CONFIG_HOME/yo-human/`, belongs to the user, and neither it nor its directory is group- or world-writable. Any other explicit file obeys the project rules: it counts only once the human has trusted its current content (`config trust`), and it may not define a `log` channel. An untrusted one exits 78 and tells the agent to hand the decision to the human. The file is opened once; its bytes, mode and owner come from that descriptor, and the path must still name it (else 78). This applies to the `--config` a `next.do` carries. The user config directory comes from `XDG_CONFIG_HOME`/`HOME`, which the caller sets; that is accepted (an agent limited to running `yo-human …` cannot set the environment; one with a whole shell can rewrite the user config itself).
14. **`config trust` and `secret set` are the human's (ruling 1, with the controller's ruling replacing the owner's `--yes`).** Each shows what it will do on the controlling terminal (`/dev/tty`) and goes ahead only when the human types `yes` there; `secret set` still reads the value from stdin, so `op read … | yo-human secret set <name>` works from the human's terminal. With no terminal (an agent), they exit 64 and say to ask the human. For the human's own provisioning scripts, `YO_HUMAN_NONINTERACTIVE=1` in the environment skips the prompt (`config trust` still prints its whole display); every such run writes `{"event":"trust"|"secret-set","noninteractive":true,…}` with the path or name, never the value, to `log.jsonl`. No usage, error, fix, notice, warning or `next` text names that variable, and no flag skips the prompt: an agent allowed to run `yo-human` can type any flag but cannot set the environment. Every fix that needs a secret or a trust now says "tell the human".
15. **Incidents are namespaced by project (ruling 5).** An incident is the pair (project, dedup key), the project being `--project`, else `YO_HUMAN_PROJECT`, else `unknown`. A repeat is suppressed, and `resolve` closes an incident, only within the caller's project; `resolve` takes `--project`, and `page --resolve` uses `YO_HUMAN_PROJECT`. *Superseded in part by amendment 25:* channels no longer receive the caller's bare key; ntfy's `sequence_id` is the message id, unchanged. `status` shows the caller's project's incidents and entries (entries of no project, such as a queued cancellation, are shown to every project) and takes `--project` and `--all`; its incident lines and objects carry `project`.
16. **The shim's default key gains a random suffix (ruling 5).** It is `orch-<UTC %Y%m%dT%H%M%SZ>-<4 hex digits>`, so two pages in one second are two incidents and the key cannot be guessed.
17. **`--project` and `--agent` are at most 64 characters (ruling 10),** and so are `YO_HUMAN_PROJECT` and `YO_HUMAN_AGENT` in their place; a longer one exits 64 (the page shim falls back to the `shim.agent` name for an over-long `YO_HUMAN_AGENT`).
18. **A `log` channel's path must be absolute (ruling 8);** a relative one is a config error (78).
19. **HOME is required (ruling 7).** A relative `XDG_CONFIG_HOME` or `XDG_STATE_HOME` is ignored, as the XDG specification says. Without an absolute `HOME`, every command except `version` exits 78 and creates nothing, unless both XDG directories are absolute.
20. **`--body-file` (rulings 3 and 4, and the controller's identity ruling).** It must be a regular file, opened once without blocking (a FIFO, device or directory exits 64 at once). It may not be the secrets file, a config layer the call reads (system, user, `--config`, project), any file under the user config directory (found by file identity, so another spelling on a case-insensitive filesystem, a symlink or a hard link does not get past), or a file whose directory is that directory under another name: 64. Every stored secret value of at least 4 bytes found in the title, ask, body, link, actions, key, project, agent or a resolve note is replaced with `REDACTED` before the message is queued, and a warning names the secret, never its value.
21. **Project files must be the user's alone (ruling 6).** A project file that another user owns, or that others can write (or whose directory others can write), is ignored with a notice, trusted or not. The search for one stops at `$HOME` and at a directory the user does not own.
22. **The trust display shows everything (ruling 2, and the controller's escaping ruling).** It renders every top-level setting and every level and send field (deadline, incident TTL, quiet hours, shim, human, ask, retry, reminder, per-send options), flags a project channel that replaces the user's channel of the same name and a project ladder that replaces the user's, shows hosts in Punycode, and lists anything it does not render under "also sets". Every key and string, at any depth, is escaped: format characters (bidi overrides and isolates, zero-width characters) and anything unprintable become `\uXXXX`.
23. **Stranded in-flight entries (ruling 9).** An entry in flight longer than max(10 × deadline, 10 minutes) is returned to the queue even if a process with its pid is alive; a younger one with a live owner is left alone, as before.
24. **The project namespace is a boundary (fix round 2, M1).** When `YO_HUMAN_PROJECT` is set (the harness sets it; an agent allowed only to run `yo-human` cannot change it), a `--project` naming another project is refused with exit 64 by `notify`, `resolve` and `status`, and so is `status --all`; the fix says to run it from that project. With `YO_HUMAN_PROJECT` unset (the human at a terminal), both work as amendment 15 says.
25. **Providers see the project's key (fix round 2, M2, and the controller's injectivity ruling; supersedes part of 15).** The incident key sent to a provider (PagerDuty's `dedup_key`, for trigger and resolve alike) is `<byte length of the project>:<project>/<key>`, for example `5:repoA/db-1`, so two projects with the same raw key have two alerts and a resolve in one never closes the other's. When that is over 255 bytes it is `h:` and the hex sha256 of that full form, which no short form (it starts with a digit) can equal. ntfy and Bark send no incident key (ntfy's `sequence_id` is the message id). Keys opened by the old paging script no longer line up with the shim's: the shim's keys are now `<n>:<project>/orch-…`.
26. **`config init --config <path>` is the human's (fix round 2, L1).** Like `config trust`, it needs a typed `yes` at the controlling terminal, or `YO_HUMAN_NONINTERACTIVE=1` with an audit line (`"event":"config-init"`); with neither it exits 64. It creates no directory: the parent must exist (64 otherwise). `config init` at the default user path is unchanged.
27. **`--body-file` stays in the project (fix round 2, L5).** Besides amendment 20, the file must lie, by identity after resolving symlinks, under the working directory or the project root (the directory holding the nearest project config file, trusted or not). Anything else exits 64 with "copy the text into the project, or pass it on stdin with --body -".
28. **Smaller fixes of round 2.** The redaction warning never names the secret, and comes only when the call sent or queued something (a noop resolve or a suppressed repeat says nothing: L2). The trust display compares against every lower layer present (system, user, a user-dir `--config` file) and says, for each secret a project channel uses, `sends secret "<name>" to <type> at <host>`, adding where the human's own config sends it when that differs (L4). `config trust` opens the file without blocking and requires a regular file (L6).
29. **The boundary between agents and the human (fix round 2, M3; documentation only).** Agents run `notify`, `resolve`, `status`, `flush` (and `levels`, `page`); only the human runs `config trust`, `config init --config` and `secret set`. The harness's permission rules are the primary control (`SECURITY.md`, `docs/setup/README.md` "Agent harness setup"); the `/dev/tty` confirmation is defence in depth that holds only when the harness gives the agent no terminal. A project file linking to another project's trusted file, or a `--config` naming it, adopts content the human trusted: routing only (L3, accepted).
30. **Flags go after the command (fix round 3, R2-M2).** `--json` and `--config <file>` are accepted anywhere after the command (after the subcommand for `config`, `secret` and `channel`); one before it exits 64 ("a flag was given before the command"), as one object when `--json` was among the arguments. So a harness's prefix rule (`yo-human secret`, `yo-human config trust`) matches every way of running a command. `yo-human --json` alone is that refusal, no longer "no command was given".
31. **The `--body-file` project root is where the project file was found (fix round 3, R2-M1; refines 27).** It is the real path of the directory in which the search found `.yo-human.yaml` (an ancestor of the working directory), never the directory a symlinked project file points to.
32. **A louder repeat or a reminder replaces the copy still held (final wave, I1).** When a repeat is sent because it is louder, or because its reminder is due, the same generation's sends that are still held are replaced, in the same hold of the incident lock that records the repeat: copies held in the queue are removed, and messages a channel scheduled (ntfy) are taken off the incident and cancelled through the channel once the repeat has gone out, or queued for cancellation if that fails. A raised page in quiet hours therefore pages once, and nothing fires when the window ends. The trade-off, accepted: the quieter held copy is removed even if the louder send then dies (exit 69). The agent is told to stop and tell the human, so the human is not silently lost.
33. **A level sends through every channel that builds (final wave, I2).** `notify` builds a level's channels one by one. A channel that cannot be built (an unset secret, a setting its type refuses) is neither sent nor queued; the result lists it as `skipped`, and its Config problem follows the result. The status counts it as not reached (`partial` when another channel delivered, `failed` when none did). The exit is 78 once nothing else is pending; while another send of the message is queued (status `queued`), the exit is 75 with `next` and the Config problem is a warning.
34. **`resolve` holds the human's alerts to account (final wave, I3 and I4; implements amendment 8).** A cancel of a scheduled send, or a close sent to a channel that can resolve (PagerDuty), that is queued for another try is a Queued problem: exit 75, with `next` (`yo-human flush`, for the entry due first). One refused for good is Unavailable: exit 69, and the fix says the page may still fire or the alert is still open. A dead failure decides the exit before a queued one. A passive note to any other channel that is queued is retried quietly and changes nothing.
35. **Help (owner, 2026-10-26; supersedes amendment 3's `--help`).** `yo-human help`, `--help` and `-h`, alone or as the word after a command (after a group's subcommand: `config check --help`), print the usage on stdout and exit 0; with `--json`, one object `{"version":1,"status":"ok","usage":"…"}`. A flag set that meets `-h` or `--help` among a command's flags does the same; a flag's value that happens to be `-h` is a value. `yo-human` with no arguments still prints the usage on stderr and exits 64. Help asked of `notify`, `resolve`, `page` or `status` leads with that command's own valid line, its flags and all, then the usage (follow-up pass, n1).
36. **Expired incidents (final wave, m10).** An incident silent for longer than `incident_ttl` is over: `status` does not list it, and `resolve` of its key is a `noop`, with no note sent. It still stops what that generation holds back (follow-up pass, R4): its queued and held entries are removed and its scheduled sends (`Reached[].Scheduled`) are cancelled, as when a new message finds the incident expired. The incident is then closed. So a hold window longer than `incident_ttl` fires nothing after the resolve. A cancel of those sends that is queued for another try exits 75 with `next`, as amendment 34 says of any queued cancel.
37. **The incident lock waits only until the deadline (final wave, m6).** A call waits for the incident lock only until its deadline. Then nothing that needs the lock is changed. A keyed `notify` sends nothing (that could page twice) and keeps the message in the queue, its sends pinned where each instance points now, for its incident check: the result is `queued` (exit 75), each channel `queued`, with `next` (`yo-human flush`), and the flush that finds the lock free checks the incident and sends what the check allows. A `resolve` changes nothing and exits 75 with `RESULT queued`; its `next.do` is the same resolve again, which is safe to repeat. A `flush` leaves what it could not check and exits 75. `status` shows the queue and warns that the incidents could not be read. Recovering stale in-flight entries never waits for the lock. A `resolve` of a key with no open incident also removes a message of that key still waiting for its incident check. Follow-up pass: a `resolve` always leaves a closed record of the incident (R1), and a waiting message written before that close is dropped when it is checked (`cancelled`, "dropped: … resolved after this message was queued"), even one a flush already claimed. A waiting message does not count as pending for the incident (R2), so it never holds back its own repeat. Recording that a send landed waits for the lock without the deadline (R3): it runs after the sends, and its work is local file writes. A delivered page therefore never exits 75 for the lock, and a lock wait that runs out after the sends is a warning on the result. `landed`, which records that a send arrived, is the one wait for the incident lock with no bound at all: the send has already reached the human, and dropping its record would leave a resolve unable to cancel or close it, so it waits for the lock however long that takes. Its hold is a few local file writes and no network call, so the wait is only as long as another process holds the lock; a holder that is stopped (Ctrl-Z, a frozen network home) can keep a delivered call past its deadline, and that is accepted over a page a resolve cannot reach. A closed record is pruned once it is older than `incident_ttl` and no queued or in-flight entry, a waiting message included, refers to it; a record a waiting message refers to is kept however old, so the drop check above always finds it. Entries in use are any queued or in-flight entry with an incident key: a waiting message, a send, a resolve note (a queued cancellation carries none and never reads an incident). Entries move between the queue and in flight without the lock (a flush's claim, and the release of a waiting message whose check found the lock busy), so the entries are listed in passes (in flight, queue, in flight) until two agree; with no stable answer, or none at all, nothing is pruned. Generations are never reused: `incidents.json` keeps `next_generation` beside the incidents, and every incident that opens takes the next one, so a key pruned and opened again starts past every entry written for its earlier incidents, and a stale entry or note of those is dropped as reopened. A file written before the counter (a bare map of incidents) is still read, the counter starting past every generation it holds.
38. **Every `--json` channel status (final wave, m7).** A result's `channels[].status` is one of `sent`, `queued`, `held`, `dead`, `skipped` (a channel this call could not build, or an entry another process claimed and is sending), `cancelled` (a queued send a resolve removed, a scheduled send cancelled, or an entry dropped because its incident moved on) and, from `channel test` only, `transient` (the test may be tried again). A waiting message's entry is listed under the channel `_admit`. `flush` carries `"id": ""`; `page` triggers add `dedup_key`, the key the page used (amendment 39).
39. **`page` prints its key (final wave, m4).** A `page` trigger prints `key: <dedup-key>` after the channel lines (`dedup_key` in `--json`), so a page sent with the default key can be resolved.
40. **`config check` prints the merged config (final wave, m9).** Each setting is one line, `<path> <- <file> = <value as JSON>`; a channel instance is the path `channels.<name>`. A secret field shows its `{"secret": "<name>"}` reference, never a value (a config holds no values). `--json` carries the merged files as `merged` beside `origins`. Defaults are not part of it.
41. **`config trust` checks the schema first (final wave, parked item).** A file that breaks the config schema would fail every command once trusted, so `config trust` refuses it (exit 78) before the prompt, and records nothing.
42. **`levels` sends what is due first (final wave, m1),** like every command but `secret`, `config` and `version`. `levels --json` carries `use_when` as one plain line, as the text form does.
43. **A resolve withdraws its key's waiting messages by name (R1 without the clock; refines 37).** While it holds the incident lock, a `resolve` lists on the closed record (`withdrawn` in incidents.json) the queue key of every message of that key still waiting for its incident check, queued or in flight (one a flush has already claimed); the queued ones are removed as before. When a waiting message is checked, it is dropped (`cancelled`) if its key is listed there, whatever the clocks say: a resolve that stamps the same instant, or a clock that stepped back, no longer lets it page. The time rule stays as a second rule, for a waiting message the listing could not see: one is written exactly when the lock is busy, so it can land while the resolve holds the lock, after the listing; it is dropped if the incident closed after it was queued. A message that starts waiting after the resolve is checked and sent. The list is kept when the key reopens, and pruned with the closed record.
44. **Every channel URL is https (EV security item).** ntfy's `server`, Bark's `server` and PagerDuty's `events_url` take the same check: an https URL with a host (a port alone is no host), without credentials, a query (even an empty `?`) or a fragment. Plain http is refused, loopback included; there is no setting or build flag that allows it. A test server is an https one, and the e2e build trusts its certificate through the test-only CA hook (`YO_HUMAN_TEST_CA`, absent from a release build, which `make nohooks` checks). `config check` refuses such a value with exit 78, naming the channel and never echoing the value.
