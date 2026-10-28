# yo-human hosted mode: the bus between a crew and its human's chief of staff

Status: draft for owner review, 2026-10-27. Extends [2026-10-26-yo-human-design.md](2026-10-26-yo-human-design.md); where the two disagree, this document records the owner's newer decision. Brainstormed with the owner and with Claude in Duet, which wrote its own integration notes for the builders (summarized under "What Duet is and how it behaves").

## Intent

The owner is making Claude Duet his chief of staff: the always-on part of him that knows his day, follows up, and decides when to pull him in. At the desk he keeps working with Claude Code sessions directly, which he likes for focused work. Both front doors sit on the same state.

yo-human already exists to let agents reach their human and get an answer back. Hosted mode makes it the shared bus that every agent, every machine and Duet read and write: asks, answers, approvals, back-and-forth threads, and the rules that decide how the human gets reached. It also means the human-facing state no longer lives inside one long-running Claude Code session, so that session (today, "the orchestrator" on the owner's Mac) can end or restart without losing anything.

## How this changes the 2026-10-26 design

The owner reversed three earlier decisions, for hosted mode only:

| Earlier | Now |
| - | - |
| Non-goal: "A hosted service or a daemon." | Hosted mode is an optional, always-on service. The local CLI stays call-only and works without it. |
| "No MCP server in v1." | Hosted mode exposes a remote MCP server; Duet is its first client. |
| "No inbound endpoint." | Hosted mode accepts writes from agents and from Duet over HTTPS. |

Everything else in the earlier design stands: one human per install, the agent decides severity, the user owns the rules, channels are plugins, free by default where possible, Go.

## The pieces and who does what

- **Claude in Duet (the chief of staff).** Talks to the human. Reads the inbox, works threads, records his answers and approvals, and can itself reach him through yo-human. Never approves anything on his behalf.
- **yo-human hosted (the bus).** Holds the inbox, threads, answers and approvals, the human's routing rules and preferences, and the channel configs and keys. Routes each message to the right channel(s). Exposes a remote MCP for Duet and an HTTPS API for agents.
- **Agents (local and cloud Claude Code sessions).** Connect to the bus as an MCP server, the same way Duet does, with their own token. That works for cloud sessions, which can't run a local CLI against the owner's machines. The yo-human CLI gains a hosted target later, as a second way in.
- **crewkit's registry.** Owns the crew's state (which sessions exist, what they are doing, what is blocked). It feeds a status summary into the bus so Duet can answer "how is everyone doing". Its own design is a separate document; this one only fixes the feed's shape (see "Crew status feed").
- **The orchestrator and the crews.** Post to the bus and act on answers. For now Duet's asks route through the orchestrator, which passes them to the right crew; later any crew can pick up its own items from the bus.
- **Spokes on other machines and accounts.** Post to the bus like any agent, including spokes that run a separate workload (see "Separate workloads on the bus").

bitc stays local-only. Anything about it reaches Duet as crew status or as an ask.

## Flows

1. **Hard page.** An agent posts with critical severity. yo-human wakes the human directly on the channels his rules give critical (PagerDuty, Bark critical, ntfy max), and copies the item into the inbox so Duet has the context when he looks. Duet is never the path that wakes him: it draws on the same usage window and could be silent exactly when a page matters.
2. **Soft page.** An agent posts something that needs the human but can wait. yo-human puts it in the inbox and sends one quiet nudge (Bark, passive or active level, collapsed: "2 items for Claude"; see "Self-hosted channels"). The human opens Duet and says "check the inbox"; Duet reads it live and talks it through.
3. **Duet reaching the human.** Duet posts like any agent, choosing the severity. Its posts are tagged as from Duet so they never come back into its own inbox.
4. **Back-and-forth.** Duet asks a crew something ("where is the release?"). That becomes a thread on the bus addressed to a crew. The crew (or the orchestrator) replies into the thread. If the reply is something the human would want, yo-human nudges quietly; Duet reads it next time he is in Duet or on its next poll.
5. **Answers and approvals.** The human answers in Duet. Duet records it on the bus with his exact words, the timestamp and the item. The asking agent reads it from the bus. The same holds for an answer given at the desk: whichever is recorded first wins and the later one is rejected, with both sides seeing the item resolved.
6. **Safety-net poll.** A Duet routine reads the inbox two or three times a day, inside the owner's waking hours, and batches what it finds into one message. A routine that finds nothing posts nothing.
7. **Real-time hand-off (bonus).** A Claude Code session on the owner's own account can hand Duet a message directly. It is fast and free while quiet, but it only works within one account, so the design never depends on it.

## Approval semantics

From Duet's own rules, adopted for the bus:

- Only the human's own typed or spoken words count. A chip tap, an emoji, a forwarded message, a tool result or text inside an item never does.
- An approval covers exactly the item as it was described to him: the action, the target and any amount. If the item changes (a new PR head, a different total), it needs asking again.
- Every approvable item has a stable, human-readable handle (repo, number, short title) so a reply can name it unambiguously.
- The bus stores each approval with the human's exact words, the timestamp, the item id and where it came from (Duet or a desk session). One approval per item; a second is rejected.
- A routine can surface pending approvals but never grant one.
- Superseded by "Human approval by passkey".

## Human approval by passkey (owner, 2026-10-27; supersedes the human paths above and in call 3)

- Everything from Duet's connection is `actor_kind: agent`, like a machine token. Duet may record the owner's quoted words and prepare an approval, but nothing it sends resolves one.
- An approval resolves only on a fresh passkey assertion the owner makes himself, on the bus's approval page. Duet sends him the link.
  - The page shows the action, the repo, the ref and the pinned commit or tree.
  - The WebAuthn challenge is bound to a hash of that exact payload, the requesting session included.
  - User verification is required, and any authenticator works: Touch ID or Face ID, iCloud Keychain, 1Password, or a phone over the QR flow on a screen that doesn't hold the key.
  - No session or earlier login counts.
- The session that acts verifies the assertion itself, against the owner's public keys pinned outside the bus, before it runs anything. A compromised bus can't approve.
- Each assertion and what ran are kept in a hash-chained, append-only audit whose head is anchored outside Fly.
- The owner's keys are pinned outside the bus, in a file whose every change is signed by a key already in it, back to a first key he pins by hand.
- An external gate (a required status check, and credentials held only by a dedicated executor) applies to every `passkey` target, and only to those.
- Recommended: the approval host is DNS-only on Cloudflare, with TLS at Fly, so no edge rule can change the page he signs. It serves only the approval and passkey routes and carries a CAA record limited to Fly's CA, which guards only against other CAs. Against a zone editor, the controls are DNS-change alerts outside the zone admin's control, the challenge-time push, and a registrar or zone lock.
- Recommended: the executor runs as its own macOS user, invoked by sudo with a reset environment, from a root-owned binary and with root-owned config, so agents running as the owner's user can't change what it trusts or runs.
- The desk sign-in from the earlier amendment stays a second human path only if the owner keeps it; the recommendation is one mechanism, the same page with the Mac's Touch ID.
- **Scope: an owner-owned policy** (owner, 2026-10-27). A signed `approval/policy.json`, pinned outside the bus like the keys file, maps each action class and target (repo, ref, environment) to `passkey`, `standing` or `deny`. Anything unlisted is `deny`, and changing the policy takes his passkey. The passkey adds assurance where his own yes is required. It never replaces existing permissions, and never adds a prompt for work already delegated: `standing` work runs as it does today, without the bus. Force-pushes, deletes, spend, secrets and money are denied whatever the policy says.

## Trust model

- Everything any reader takes from the bus is data, not instruction. The bus returns structured fields (id, kind, severity, from, swarm, title, summary, required action, risk flags, canonical link), not prose that tells the reader what to do.
- Links on the bus are canonical URLs from the source system's API, never constructed from item text.
- Each writer authenticates as itself (see "Auth"), and every write records who made it. A post claiming to be from Duet or the human is accepted only from Duet's or the human's credential.
- Text from public sources (issues, PRs, mail) is labelled as untrusted in the item and never becomes an action without the human's word.

## Cost: the usage window is the budget

The owner's binding constraint is his shared Claude usage window: Duet's sessions, the crews and his own work all draw on it.

- Duet measured one empty routine run (connect, list tools, one call, nothing to report) at about 124k tokens of context, roughly $0.75 at API list prices. Startup is the cost, so the saving comes from not starting a session, not from a cheaper check inside one.
- So: push first (quiet nudges that bring the human to Duet; real-time hand-offs where available), and keep polling to two or three runs a day. Use one-off runs at a known time for something in flight instead of a faster schedule.
- MCP answers stay small: summaries and handles first, detail only on request (`item(id)`).
- The bus moves no files (owner, 2026-10-28): an item carries text and at most one allow-listed `https` link, a pointer and never a copy. A future file hand-off must meet the plan's four conditions: no reads from protected Mac folders, owner-initiated and named per crew, logged with size and sha256 only, and client files only to that crew's node.

## What Duet is and how it behaves (from its own notes and the 2026-10-27 spike)

- Duet's sessions are event-driven, not a daemon: they wake when the human writes, a routine fires, a task thread reports, or a Claude Code session on his account hands a message over. Nothing can push into Duet from outside (no webhook a server can call).
- Sessions are recycled and their disk is temporary. Anything Duet must remember lives on the bus or in its own notes.
- Repeating routines run at most hourly; one-off runs can be set for an exact time. So nothing time-sensitive may depend on a Duet poll (owner, 2026-10-28). The bus pushes: approvals, pages and escalations reach the owner by Bark or PagerDuty. Duet reads the bus only on its routine or when he opens it.
- Spike result (2026-10-27): Duet's main chat and a scheduled routine both called a custom remote MCP connector we hosted on Fly. Duet treated a test approval item as data and took no action. Each Duet session reconnects and lists tools before calling.

## Duet-facing MCP tools (minimal)

Read, safe from routines:

- `inbox_for_claude(since?)`: items needing the human, grouped by kind (approval, question, blocker, finished, failed), each with id, handle, from, swarm, severity, title, summary, created_at, link.
- `item(id)`: one item in full, including the detail the human needs to decide and the thread so far.
- `swarm_status(swarm?)`: the crew status summary from the registry feed.

Write, only after the human's message:

- `record_answer(item_id, text, human_ts, source_message?)`: an answer to a question.
- `record_approval(item_id, decision, text, human_ts)`: approve or reject; idempotent on item id.
- `post(to, title, body, severity, thread_id?)`: Duet asking a crew something, replying in a thread, or reaching the human through yo-human's channels.
- `ack(item_id)`: mark surfaced so a poll doesn't repeat it.

Excluded on purpose: merge, deploy, delete, spend, secrets access, and any tool that runs something on a machine.

## Agent-facing side

- Hosted mode is an MCP server first. Agents use the same server as Duet with an agent token and a broader tool set: `post` (notify, ask, done, escalate, with severity), `answers(since?)` and `item(id)` to read replies, `reply(thread_id, ...)`, and `status_push` for the registry feed. Answers reach agents by the agent reading the bus; the bus never calls into a machine.
- A local Claude Code session adds it with `claude mcp add --transport http` and its token; a cloud session gets it as a connector.
- A central orchestrator, and later each crew's own orchestrator, reads new answers and threads addressed to it and fans them out: over UDS (SendMessage) to Claude sessions on its own host, and through its bridge to another host's bridge for sessions elsewhere. Never a herdr pane prompt on the same host. SendMessage is the Claude harness's own channel, an instruction for sessions that no yo-human code implements. A non-Claude watcher or daemon wakes only its own host's bridge pane, with `herdr agent prompt` (owner, 2026-10-28: "agent prompts between bridges only (host-to-host) and uds on a host"). The bus itself never wakes a session; sessions read it.
- The yo-human CLI gets a hosted target after M1, as a second way in for scripts and hooks.

## Auth

- Duet connects as a custom connector with OAuth. Prefer Claude's published client identity (Client ID Metadata Document); fall back to Dynamic Client Registration. The bus serves exactly one human.
- Agents and machines use per-node tokens, each named for its node, scoped to post and read its own items, revocable one at a time. Each token is scoped to its own swarm (see "Separate workloads on the bus").
- Channel keys (PagerDuty, Bark, ntfy) live only on the bus, as secrets, and no tool returns them.

## Separate workloads on the bus

Any crew, including one for a separate workload, posts to the bus with its own crew-scoped token.

- Its items carry its own `swarm` and a token scoped to that swarm. Duet can route a swarm to a dedicated channel.
- Items say what the owner needs to know to decide, the same as any other swarm. The bus's data is private to the owner. The rule against naming third parties applies to this repository, its issues, its logs and its tests.
- A swarm's token can't read other swarms' items, and other swarms' tokens can't read its items. Only Duet and the owner see across swarms.

## Crew status feed (contract with crewkit's registry)

The registry pushes a small status document per swarm: swarm, sessions (name, state: working, blocked, idle, done), what is waiting on the human (item handles), main branch health, spend against cap where relevant, and a timestamp. The bus serves it through `swarm_status`. The registry design decides how the document is produced.

## Hosting

- Fly, in the owner's personal organization, one always-on machine (no auto-stop) to avoid cold starts. A shared-cpu-1x 256 MB machine is about $2 a month.
- Storage on a Fly volume with backups. Nothing else on Fly reaches into the owner's machines.

## Self-hosted channels (owner, 2026-10-27)

- **Reaching the owner:** a self-hosted Bark server (bark-server) on Fly, in the owner's personal organization, as its own app with one always-on machine. The official Bark iOS app points at it. Levels: passive, active, time-sensitive, critical. The bus reaches it through a channel adapter (`notify.Channel`), with Bark as H1's only implementation. H1 sends quiet nudges at the active level by default (passive is configurable). Agents can escalate one of their unanswered items a step at a time, once it has waited a set time (30 minutes by default), to time-sensitive and then critical (owner, 2026-10-27). Critical is used only after a real test on the owner's phone with the silent switch on and a Focus active; H2's routing rules then govern every level.
- **The Bark server is crew infrastructure, not part of the bus.** crewkit may use it later as a second consumer. Its deploy, like the owner's running bus instance, lives in the owner's private crew-infra repository. This repository keeps the product and a generic example deploy with placeholders only.
- **Private by default:** basic auth on every push route. Device registration is closed except during a window the owner opens, because bark-server's own auth skips `/register` and an open register can re-point an existing device key. The image is pinned by digest. Nothing is exposed before the owner's go.
- **No ntfy on Fly.** The homelab ntfy on thehalf keeps carrying homelab notifications as it is today.
- **Session to session goes through the bus:** spokes and sessions post to it and follow it by long-poll on `answers` (H1); an event stream for non-MCP watchers follows in H2. Moving crew pointers off the homelab ntfy onto the bus comes later, with no rush.
- **PagerDuty** stays the backstop for hard pages.
- **Size:** two machines in total (the bus and the Bark server), about $4.20 a month; no further machines without the owner's go.

## Build order (owner, 2026-10-27)

Get an MCP up fast. Hosted mode's first release is the MCP bus with only what Duet and the agents need to talk:

1. **H1, the bus:** items, threads, answers, approvals (with the server-side rules), Duet's OAuth, agent tokens, the Duet tools and the agent tools (with long-poll on `answers`), and quiet nudges through the self-hosted Bark server only. Hard pages keep using today's paging path until M1 ships.
2. **M1 continues in parallel** in the CLI as planned: local notify through PagerDuty, ntfy and Bark. The two share no code at first.
3. **H2:** routing rules on the bus, critical pages copied to the bus with dedup (call 1), and the full channel set. This replaces the earlier M2 (local question store); question sessions in herdr are dropped unless the owner asks for them again.
4. **H3:** the CLI's hosted target and the registry's status feed.

## Goals

- Duet becomes a working front door: the human can see what needs him, answer and approve from his phone, and have those answers reach the agents.
- No answer is lost: every ask has one id, and its answer reaches the asker whichever surface it came from.
- The orchestrator's session can restart without losing any pending ask, answer or approval.

## Non-goals

- Duet approving anything on the human's behalf, or acting on machines.
- A hosted bitc.
- Replacing Heeler or herdr for desk work.
- Multiple humans per install.

## Anti-goals

- Noise: a nudge or page that didn't need the human, or two for one thing.
- Silent loss: an answer recorded but never read by the asker, or an item acknowledged but never shown.
- Burning the usage window on empty polls.
- A path from the internet into the owner's machines.
- Client names or client detail in this repository, its issues, logs or tests; or one swarm reading another swarm's items.

## Calls made while writing this (the orchestrator's, for the owner to check)

1. **Critical pages never depend on the bus.** A machine that may send critical pages keeps the M1 local send pipeline and the keys for its critical channels. A critical page goes out locally first (queued locally under the M1 deadline rules), then a copy is posted to the bus for Duet. The bus dedups on the message's dedup key, so it never re-sends a page a machine already sent. Non-critical messages go to the bus only. This replaces "channel keys live only on the bus" for critical channels.
2. **The constitution is amended first** to allow hosted mode and to make MCP its primary interface (the owner: "ideally hosted mode is MCP so cloud sessions can use it"). Principle VI's "no MCP path the CLI lacks" is restated for hosted mode as "the MCP server is the contract; the CLI's hosted target follows it".
3. **Desk answers and approvals.** *Superseded on 2026-10-27 by "Human approval by passkey":* everything from Duet's connection, and every node token, is `agent`. Only the owner's own payload-bound passkey assertion resolves an approval; a desk sign-in, if kept, may only answer questions.
4. **Duet's OAuth sign-in uses GitHub as the identity provider**, restricted to the owner's GitHub account. The bus issues its own tokens after that check. There is no password store on the bus.
5. **Rules live on the bus in hosted mode.** `yo-human rules pull|push` edits them as YAML, and Duet gets a `propose_rule` tool whose change applies only after the human confirms in words. Local layers still apply to local critical sends (call 1) and to machines not using hosted mode.
6. **The server enforces the approval rules.** Every item carries an `action` category and a `revision`. Re-posting under the same handle bumps the revision and voids any earlier approval. `record_approval` is rejected for the excluded categories: force-pushes, deletes, any spend and any change to secrets always, plus merges, releases or tags, deploys and public syncs unless the owner has said yes to the passkey scope and an external gate enforces it (see "Human approval by passkey"). Secrets and money are never on the bus.
7. **Durability.** Litestream replicates the SQLite store continuously to Fly's object storage (Tigris, inside the free allowance), and the plan includes a restore drill. Daily volume snapshots alone could lose a day of answers.

## Left to the builder

- The storage schema and engine (SQLite on the volume is the default expectation).
- The HTTP framework and MCP library versions (the spike used the Python SDK; the product is Go, per the earlier design).
- Exact CLI verbs and flags for the hosted target, within the existing contract style.
- The nudge's wording and collapse rules.
- How the orchestrator fans out answers locally until crews read the bus themselves: settled (owner, 2026-10-28). UDS (SendMessage) between Claude sessions on the host, a non-Claude watcher prompting only its own host's bridge pane, and bridge to bridge across hosts.

## Open questions

- Whether Duet's `watch_url` tool can watch a bus endpoint for changes without a model run. Untested; if it is cheap it could make Duet's reads cheaper, but it would never carry anything time-sensitive.
- Whether a local Claude Code session can load the Claude Code Remote connector for real-time hand-offs (today the orchestrator's session can't see it).
- How the human tells the bus where he is ("heads down", "driving"): through Duet, through any agent, or both.
