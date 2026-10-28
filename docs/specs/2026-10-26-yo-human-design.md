# yo-human: design

Status: draft for owner review, 2026-10-26; owner answers to the open questions recorded 2026-10-25 (see "Decided by the owner"). The repo's rules are in [../../constitution.md](../../constitution.md). Part of the crewkit family (crewkit, bitc, memsync). Open questions a builder needs answered first: [../OPEN-QUESTIONS.md](../OPEN-QUESTIONS.md). Plan: [../plans/2026-10-26-plan.md](../plans/2026-10-26-plan.md).

## Intent

yo-human is the interface channel between a crew of AI agents and their human. It exists so the human can be away from the keyboard, never the blocker and still a full participant: answering questions, approving actions, steering and handing out new work from a phone. It gives agents the proper tools to go "yo, human!" and to do it well.

The owner's framing: "the away from my desk thing is kinda why I'm building the toolkit." yo-human is the piece that makes it real.

## Who it serves

1. **The side-project worker:** at a day job, running personal agents and projects from a phone. Needs discreet, low-noise notifications and quick answers between meetings.
2. **The off-hours guardian:** uses it for work while off, to know everything is OK. Mostly silence, then a truly loud page when something breaks.
3. **The away-from-keyboard worker:** uses it to be at the keyboard less and live more. Needs the richest two-way: approve, answer, steer, assign from the phone.
4. **The personal-agent power user:** wants agents to have many ways to reach them. Needs pluggable channels and rich, living rules.

## Goals

- **Agent tools.** A CLI (an MCP server only later, if a runtime needs one) agents call to notify, ask, report done and escalate, with clear rules for which call fits which moment, in the bitc style: stable exit codes, a documented contract, a skill that teaches agents to use it.
- **Questions as first-class objects.** `ask` creates a question with context, options, importance and the asker. The answer goes back to the asking agent. One question has one ID across every channel.
- **Question sessions (proposed mechanism).** For a question that needs the human, yo-human can open a short-lived, dedicated herdr session for it (for example `q-<agent>-<topic>`) holding just the context and options. It sits Blocked, Heeler shows and pushes it, the human answers in Heeler's chat, the answer is relayed to the asker, and the session closes. This reuses herdr plus Heeler for the full-participant experience instead of building an app.
- **Notifications at PagerDuty level.** Levels and urgency, escalation policies, quiet hours, per-project and per-agent rules, dedup and collapse, acknowledgement and resolve, and delivery confirmation where the channel supports it.
- **Living, user-owned configuration.** The user sets and changes behaviour by talking to any agent ("I'm going for a walk, text me when it's done"; "don't buzz me for FYI"; "page me if nobody answers in 10 minutes"). The agent writes it as a rule, scoped and optionally expiring, that every agent and the router see. yo-human ships mechanisms, not preferences.
- **Pluggable channels.** Channels are plugins with a manifest and declared capabilities. Day one: PagerDuty (critical, SMS and phone backup via its free plan), ntfy, Heeler (push, plus the question sessions) and email or a local log. Later: Twilio and other SMS or voice, chat apps, anything a third party adds.
- **Rich for the medium.** Each message renders as richly as its channel allows: Heeler gets buttons, reply and the question session; SMS gets text plus a short reply code; a phone call gets a spoken summary with keypad choices; PagerDuty gets an incident with details and ack/resolve.
- **Context-aware routing.** The router matches a message (importance, needs-answer, project, agent) against the current rules and context and the channels' declared capabilities to choose how to reach the human. Example: a normally quiet "done" may go through PagerDuty's critical path when the context says "on a walk, phone silent, text me", because that is the only channel that will actually reach them.
- **Upstream contributions to Heeler.** Heeler (Apache-2.0) is the preferred app. Its notifications are weak today. Contribute what yo-human needs: agent-authored messages with levels, time-sensitive interruption level, notification actions, ideally reply from the notification. Our contribution fork is a planned fork of Heeler.

## Non-goals

- **Building our own app.** The owner likes Heeler and uses the official app. No separate app, TestFlight pipeline or app CD.
- **Defining user preferences.** No hard-coded policy for what happens when a question goes unanswered, quiet hours and so on. Ship mechanisms plus a setup conversation that helps each user choose; the user owns the choices.
- **A hosted service or a daemon.** yo-human runs on demand on the user's machine alongside their agents. Channels may call hosted providers (PagerDuty, ntfy.sh), but yo-human itself is not a SaaS.

## Anti-goals (failure even if it "works")

- **Noise.** Paging the human for things that didn't need them, or paging twice for one thing. Every page must say what it needs from the human.
- **Silent loss.** A question or notice dropped without anyone knowing: an answer that never reaches the asker, a notice marked seen but never shown.
- **Blocking agents or prompts.** A slow channel or hook must never stall an agent's turn; yo-human answers fast or fails silent and retries.
- **A security hole.** A channel plugin that can read other channels' secrets or run with full trust (the OpenClaw lesson). A reply channel that lets someone other than the human answer.

## Constraints

- **Free by default.** Built on free services: PagerDuty Free for critical (iOS Critical Alerts, 100 SMS/phone notifications a month), ntfy, Heeler's relay. Paid channels (Twilio and others) are optional plugins added later.
- **iOS Critical Alerts** (bypass the silent switch) require Apple's entitlement per app. PagerDuty's app has it. Heeler would need its maintainer to request it; there is no sideload or TestFlight route around it. Time-sensitive alerts (break through Focus, not the silent switch) need no approval.
- **Performance.** yo-human runs on demand, with no daemon. A send happens inline under a hard, user-configurable deadline (default 1 s); on timeout or failure the message stays queued and the call exits with a "queued" code. Commands with no network work have CI-enforced, ratcheting budgets in the tens of milliseconds.
- **Discipline, as for every crewkit repo:** strict test-first with recorded RED, a coverage ratchet, performance budgets, docs as you go (inputs-first and DRY setup docs), GitHub-hosted runners only, actions pinned by SHA, no `pull_request_target`.
- **Codenames.** Public text names no client.
- **Reply authenticity.** Only the human can answer. Inbound replies are verified per channel (signatures, PagerDuty user identity, Heeler's per-host keys).

## Decided by the owner (2026-10-25/26)

- Name: **yo-human**, for the product and its CLI. App-side subtitle idea "Middle management for your AI".
- Heeler is the app. Expand it upstream; no app of our own. The fork is only a contribution vehicle.
- yo-human, the backend and interface channel, is the real product, in its own repo.
- PagerDuty carries critical, SMS and phone for now; Twilio and others later via plugins.
- Notifications should reach PagerDuty-level customization, plus critical alerts.
- Channels must be plug-and-play for other people, with messages as rich as each medium allows.
- Behaviour is configured by the user, conversationally, through agents. The system is "living".
- Upstream improvements to Heeler; a sync job keeps our fork current (a scheduled GitHub Action where Claude summarizes upstream changes in an issue and opens a sync PR, never merging).
- Open questions answered (2026-10-25; details in [../OPEN-QUESTIONS.md](../OPEN-QUESTIONS.md)):
  - v1 is the off-hours guardian: outbound notify (PagerDuty, ntfy, Bark), dedup, resolve, quiet hours, a drop-in shim for the existing paging script. `ask` and question sessions come after v1.
  - Call-only, no daemon: inline send under a deadline, local queue, flush on the next call or from cron. No inbound endpoint.
  - The core works without herdr or Heeler. v1 doesn't wait on upstream Heeler work.
  - One human per install, with a `human` id on everything.
  - yo-human becomes the only way agents page the human.
  - Go. Claude Code skill plus the CLI at launch; no MCP server in v1.
- No app of our own for Critical Alerts (2026-10-25): the entitlement is unlikely for a personal agent-alert app and a new app can't reuse ntfy's push path. Instead add **Bark** (free, MIT, self-hostable, has Critical Alerts, one-way) and **Pushover** ($4.99 once, Critical Alerts on priority 2, receipts for ack read-back; an optional paid channel) as channels. Verify Bark's critical level on a real device (silent switch on, Focus active) before relying on it.

## Channel plugins (direction, from the OpenClaw research)

- **Manifest** (`yo-human.channel.json`): id, apiVersion, a config JSON Schema with secret fields marked, compat (minimum core version), readable without running code.
- **Closed, core-owned capability set:** text, markdown, buttons or choices, reply, media, urgency levels including break-through and critical, delivery confirmation, acknowledgement, quiet-hours awareness. The router matches on these; plugins can't invent new ones.
- **Small interface:** `send(message) → deliveryId`, optional inbound (webhook or poll) with the plugin verifying authenticity, `health()`, `testSend()`.
- **Core owns correlation IDs** (question, delivery). A plugin maps provider IDs to them, so an answer from any channel resolves the same question.
- **Isolation:** plugins run out of process with only their own secrets (secret references, never other plugins'), and a declared outbound-host allowlist.
- **Supply chain:** pinned versions with checksums; no open registry at launch; a "verified" tier later.
- **Quick setup:** an Apprise-style URL shorthand (`pagerduty://…`, `ntfy://…`) for adding a channel.
- **Quality tier per channel** (does it confirm delivery, carry replies, break through silent?), so the router knows what it can escalate to.

## Mechanisms the user configures (by conversation)

The design provides these knobs. The setup agent proposes defaults with the human; nothing is fixed in code.

- Levels and what each means for this user.
- Escalation policies: what happens when a question is unanswered (keep waiting, escalate after N minutes to another channel, let the agent take a stated safe default), per level, project or agent.
- Context rules with optional expiry ("on a walk until I say I'm back", "in a meeting until 3pm", "on call this weekend"), and how the user signals they're back (says so, expiry, or activity in Heeler or herdr).
- Quiet hours and do-not-disturb, with what may break through.
- Per-project and per-agent overrides.
- Which channels exist and their order of preference for each kind of message.

## Left to the builder

- ~~Language and runtime~~: decided, Go (same as bitc and memsync; a static binary starting in milliseconds suits a call-only CLI).
- Storage for questions, rules, context and delivery state (likely a small local SQLite or file store with atomic writes).
- The exact CLI verbs, exit codes, MCP tool shapes and the agent skill text.
- ~~How question sessions are created and torn down in herdr, and how the answer is relayed back to the asker (herdr API, SendMessage, a file the asker waits on).~~ Superseded (2026-10-28): hosted mode drops herdr question sessions (hosted-mode spec, "Build order", H2). Answers reach the asker through the bus, and sessions on one host message each other with SendMessage (hosted-mode spec, Agents).
- The Heeler integration surface: what works today through its plugin and relay, and what needs upstream changes.
- The rule language for conversationally set rules, and how agents write and validate them.
- Test harnesses: fake channels, a fake PagerDuty, a fake clock for escalation.
