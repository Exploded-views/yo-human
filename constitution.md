## Amendment Log

### v1.1.0 — 2026-10-27
- **Amended the preamble and Principles IV, V, VI, VII and VIII**: hosted
  mode is allowed as an optional always-on service, with its own layer chain,
  single write path and single network path. Its MCP server is the contract.
  Tool calls are deadline-bound. Answers and approvals follow first-wins and
  revision rules, and only the owner's own payload-bound passkey assertion
  resolves an approval; the acting session verifies it against keys pinned
  outside the bus. Every request is authenticated, and crews are isolated
  (#68)
- **Scope (owner's answer of 2026-10-27, #72)**: not a fixed list but an
  owner-owned approval policy (Principle VIII). Each action class and target
  is `passkey`, `standing` or `deny`, and anything unlisted is `deny`. The
  default policy keeps today's delegations as `standing`. Changing the policy
  takes the owner's passkey. Force-pushes, deletes, spend, secrets and money
  are always denied, whatever the policy says. The passkey adds assurance
  where the owner's own yes is required; it never replaces existing
  permissions or adds prompts for work already delegated
- **Rationale**: the owner reversed "no hosted service", "no MCP server" and
  "no inbound endpoint" for hosted mode on 2026-10-27
  (`docs/specs/2026-10-27-hosted-mode-design.md`, call 2). A compromised bus
  can still mislead the page; the acting session's own payload check is what
  limits the damage
- **Impact**: `CONTRIBUTING.md` here. In H1 Task 2: the hosted depguard
  rules and their probes (`.golangci.yml`, `scripts/lint_rules_test.sh`), the
  token gitleaks rule, the `Makefile` targets and `CLAUDE.md`. The CLI's
  contract is unchanged

### v1.0.2 — 2026-10-27
- **Amended the preamble, Principle IV's layer diagram and the free-services principle**: the v1 channels are PagerDuty, ntfy and Bark; Heeler is a later channel (#71)
- **Rationale**: the text still named Heeler as a v1 channel, while the owner's decision (`docs/OPEN-QUESTIONS.md` Q1) and the M1 contract name Bark. Owner approved 2026-10-27 (relayed by the orchestrator, #71)
- **Impact**: `CLAUDE.md`, the roadmap and `docs/OPEN-QUESTIONS.md` updated in the same change; no code or CI change

### v1.0.1 — 2026-10-26
- **Amended Principle IV**: a channel implementation may import its provider SDK only once the SDK has been reviewed and named in the depguard allow list (#60)
- **Rationale**: the wording allowed any provider SDK, while the depguard `channel-implementations` rule already rejects SDKs until they are named; the text now matches enforcement
- **Impact**: none; `.golangci.yml` already enforces this, and no channel uses an SDK

### v1.0.0 — 2026-10-25
- Initial ratification.

<!--
Amendment entries go here, newest first. Format:

### vX.Y.Z — YYYY-MM-DD
- **Added/Amended/Removed Principle N**: short description
- **Rationale**: why the change was needed (the incident, the drift, the new constraint)
- **Impact**: what existing code / docs / CI config must change

Semver:
  MAJOR — principle removed or redefined incompatibly
  MINOR — new principle or material expansion of an existing one
  PATCH — wording/clarification, no semantic change
-->

---

# yo-human Constitution

yo-human is the one way a crew of AI agents reaches their human. It is a Go
CLI that agents call (`notify`, `resolve`, and later `ask`) and that delivers
through pluggable channels: PagerDuty for critical, SMS and phone; ntfy; Bark;
later Heeler.
The CLI runs on the user's own machine, on demand, with no daemon. Hosted
mode is optional: one always-on service (`yo-human-hosted`) that the human runs
for himself, holding his inbox, threads, answers and approvals, and reached by
agents and by his chief of staff over MCP. The CLI works without it.

Getting it wrong is costly in three ways the human cannot see from
their phone: a page that never arrives (production is down and nobody knows),
a page that arrives twice or for nothing (the human learns to ignore it), and
an agent stalled on a slow channel (the work stops while the human is away,
which is exactly what yo-human exists to prevent). Every principle below
protects one of those three.

---

## Core Principles

### I. Test-Driven Development (NON-NEGOTIABLE)

Tests MUST be written before implementation code, and the failure MUST be
proven.

- Write the test, run it, and record the RED output (the failing assertion,
  not a compile error) in the PR or commit message
- Commit the failing test before the fix; then the minimum code to pass;
  then refactor with tests green
- After GREEN, run a mutation check on the key line: change it, confirm a test
  fails, revert. A mutant that doesn't compile does not count as killed
- Coverage MUST NOT fall below the per-package floor recorded in the coverage
  ratchet file; floors only rise
- Test command: `make test` (`go test -race ./...`)
- Channels are tested against fakes (fake PagerDuty, fake ntfy, fake clock),
  never against live services in CI

**Exceptions**: Exploratory spikes MAY skip tests but MUST NOT merge until
rewritten test-first. The M0 scaffold (Makefile, CI, ratchet files) is
exempt while the test harness itself is being built; everything after it is
not.

**Rationale**: yo-human's worst failures happen silently and on the human's
phone, not in a terminal anyone is watching. A dedup bug that pages twice, or
a queue bug that drops a notice, shows up as a 3am page or as nothing at all.
Recorded RED proves the test can actually catch the bug. Mutation checks
exist because tests that pass whatever the code does have already slipped
through in this family of repos.

---

### II. Quality Gates and Ratchets

All code MUST pass these gates before merge. CI runs them on GitHub-hosted
`ubuntu-latest` and `macos-latest` (agents run on Linux hosts and macOS
laptops):

- `gofmt` (no diff)
- `go vet ./...` and `golangci-lint run` (v2 config in `.golangci.yml`,
  including `gocyclo`, `gosec`, `errorlint` and `depguard` for Principle IV)
- `go test -race ./...` with the coverage ratchet
- Performance budgets (Principle VII), enforced as a ratchet
- `gitleaks` over the full history (`make secrets`)
- Workflow lint: crewkit's `tools/check-workflows.py`, pinned by sha256
- `govulncheck ./...` (`make vuln`): advisory per PR, blocking at release

Rules:
- `make ci` runs exactly what CI runs; a lefthook pre-commit hook runs the fast
  subset (`gofmt`, `go vet`, `golangci-lint`, `go test` on changed packages)
- Ratchets only tighten. Raising a time budget or lowering a coverage floor is
  a constitution amendment, not a PR change
- Gate failures block merge. `--no-verify` needs a written reason in the PR
- Check exit codes directly; never pipe a gate through `tail`, `head` or `grep`
  in a way that hides its status
- GitHub Actions: hosted runners only, every action pinned to a full commit
  SHA, never `pull_request_target`

**Rationale**: yo-human is built mostly by agent sessions working in
parallel, and the gates are the only baseline they all share. A pipe through
`tail` has already hidden a failing exit code in this family of repos, and an
allow-only policy has already enforced nothing. Ratchets turn each gain in
coverage or speed into a floor instead of a number that slowly slips back.

---

### III. File Size and Complexity Limits

Go source files MUST NOT exceed **400 lines**.

- Files approaching 300 lines SHOULD be split during review
- Cyclomatic complexity MUST NOT exceed **10** per function (`gocyclo`,
  `min-complexity: 10`)
- Split along responsibility boundaries: one channel, one command, one store
  concern per file, never arbitrary cuts
- **Exemptions**: generated files, `testdata/` fixtures. `_test.go` files are
  held to the same limit

**Rationale**: Each channel, the router and the dedup logic must be small
enough for a reviewer to hold the whole decision in their head, because the
security-minded review (Principle VIII) and the never-page-twice review
(Principle V) are only as good as what the reviewer can see. The design
already expects many small channel plugins. A 1,000-line router is where a
"page twice" bug hides.

---

### IV. Layer Boundaries

Code MUST follow this one-way dependency chain:

```
cmd/yo-human → internal/cli → internal/core → internal/store
                                   ↓
                            internal/channel (interface + closed capability set)
                                   ↑
                            internal/channel/<name> (pagerduty, ntfy, bark, log)
```

- Channel implementations MUST import only `internal/channel` and the standard
  library, or their provider SDK once it has been reviewed and named in the
  depguard allow list. They MUST NOT import `core`, `store` or another channel
- `internal/cli` parses arguments and maps results to exit codes; it MUST NOT
  contain routing, dedup or delivery logic
- **Single write path**: all persistent state (queue, dedup keys, delivery
  state, later questions and rules) is written only through `internal/store`,
  atomically (temp file plus rename, or a transaction) under a lock
- **Single network path**: all outbound network traffic goes through a channel.
  `core` makes no network calls
- `depguard` rules in `.golangci.yml` enforce the import rules in CI

**Hosted mode** has its own chain, and shares no code with the CLI's:

```text
cmd/yo-human-hosted → internal/hosted/app → internal/hosted/{web, oauth, nudge, auth}
                                          → internal/hosted/bus (domain rules)
                                          → internal/hosted/db  (implements bus.Store)
                                          → internal/hosted/egress/<name>
```

- `internal/hosted/bus` imports only the standard library
- **Single write path**: all hosted state is written only through
  `internal/hosted/db`, inside a transaction
- Inbound HTTP lives only in `internal/hosted/web` and `internal/hosted/oauth`
- **Single network path**: outbound traffic goes only through
  `internal/hosted/egress/<name>`, which import only the standard library and
  `internal/hosted/notify` (the channel adapter interface)
- Hosted packages and CLI packages never import each other
- Third-party modules (the MCP SDK, the SQLite driver) are allowed only once
  reviewed and named in the depguard allow list, and only in the package that
  needs them

**Rationale**: The design puts channels out of process later, with only their
own secrets (Principle VIII). That move is only possible if channels never
reach into core or into each other today. Many short-lived calls hit the
store at once, so a second write path is how the queue gets corrupted.

---

### V. Delivery Integrity: Never Lost, Never Twice (NON-NEGOTIABLE)

Every message an agent hands to yo-human MUST be either delivered or visibly
queued, and MUST reach the human at most once per incident.

- A message is written to the local queue before any send is attempted. It
  leaves the queue only when a channel confirms delivery
- Every `yo-human` call first sends anything still queued, within the same
  deadline (Principle VII); `yo-human flush` does the same on demand or from
  cron
- The same dedup key means the same incident: a repeat updates or collapses
  the existing one and never pages again. `resolve <dedup-key>` closes it
- Retries are bounded, with backoff recorded in the store. A message that
  runs out of retries is kept in a dead-letter view that `status` shows,
  never deleted
- No silent error swallowing: every error is either handled specifically or
  returned. A failed send is never reported as delivered
- Every page states what it needs from the human
- **Hosted mode**: a write is committed before it is acknowledged. The first
  human answer or approval recorded for an item's revision wins; a later one
  is rejected and shown the winner, and a byte-identical retry returns the
  recorded one. An answer recorded by an agent (`actor_kind: agent`) never
  wins over a human one: a later human answer supersedes it. A nudge goes
  out at most once per window. Missing one is covered by the scheduled
  poll; a duplicate is noise

**Rationale**: These are the design's anti-goals: silent loss and noise. A
hook that timed out after marking notices "seen" has already lost notices
in this family of repos (memsync). The paging script yo-human replaces had no
shared dedup rule, so one broken health check could page again on every
agent turn. Once the human stops trusting the page, yo-human is worthless.

---

### VI. The CLI Is the Contract

The CLI's verbs, flags, exit codes and stdout formats are the public API
that every agent runtime depends on.

- Exit codes are stable and documented, following bitc's sysexits style where
  the meanings match (0 done, 64 usage, 69 unavailable, 75 transient or queued,
  78 bad config). Delivered and queued MUST have different exit codes
- Machine-readable output (`--json`) is versioned. Human-readable output may
  change; machine-readable output may not without a major version
- The drop-in shim for the existing paging script keeps that script's flags,
  including `--resolve <dedup_key>`
- Breaking changes to verbs, flags, exit codes or JSON need a CHANGELOG entry
  and a major version bump, and the agent skill must be updated in the same PR
- No runtime-specific path (MCP or similar) may offer behaviour the CLI lacks,
  except in hosted mode
- **Hosted mode**: the MCP server is the contract. Tool names, input and
  output schemas and error codes are versioned (`"v"` in every result). A
  breaking change needs a new `v`, a CHANGELOG entry and the skill update in
  the same PR. The CLI's hosted target follows the MCP contract

**Rationale**: Agents learn the contract once, from a skill, and act on exit
codes without reading prose. A renamed flag or a reused exit code doesn't
fail loudly: an agent misreads "queued" as "delivered" and stops trying.
Claude Code is first, but the CLI is how every other runtime takes part.

---

### VII. Never Block the Agent

A yo-human call MUST return within a bounded, configurable deadline,
whatever the channels are doing.

- Network sends happen inline under a hard deadline (default 1 s, set by the
  user in config). On timeout or failure the message stays queued (Principle V)
  and the call exits with the "queued" code. It never waits longer
- Commands that do no network work (`status`, enqueue, config validation)
  have p95 budgets in the tens of milliseconds, recorded in the performance
  ratchet file and measured over N runs with a recorded headroom factor
- Hooks or skills that call yo-human set their own deadline below the host's
  hook timeout and change no state when they overrun
- A slow path is fixed properly. Raising a timeout or budget to make CI pass
  is not a fix (Principle II)
- **Hosted mode**: every tool call runs under a server-side deadline (default
  5 s, configurable), and a long-poll has its own cap that the caller chose.
  Channel sends happen in a worker after the write commits, never inside a
  tool call

**Rationale**: yo-human exists so the human is never the blocker. A
notification tool that blocks the agent recreates the same problem one level
down. Claude Code throws away hook output after its timeout (10 s here), and
IO contention when many sessions wake at once has already pushed a 0.3 s hook
past 10 s. Budgets have to hold in the worst case, not the average.

---

### VIII. Secrets, Channel Isolation and Reply Authenticity (NON-NEGOTIABLE)

No secret is ever committed, printed or shared across channels, and only the
human can answer or approve; in hosted mode an agent may record the human's
answer to a question as `agent`, which grants nothing.

- Routing keys, API tokens and passwords live in files outside the repo or in
  1Password. Config holds references, never values. `.env` and local config
  are gitignored
- Credentials are piped, never pasted: `op read "op://<vault>/<item>/<field>" | <consumer>`
- Commands, logs, errors and test output report secret names and counts only,
  never values or byte-identical fragments
- Each channel gets only its own secrets. One channel MUST NOT be able to read
  another's, now in process and later out of process
- Every inbound reply or acknowledgement is verified per channel (signature,
  PagerDuty user identity, Heeler host key) before it changes any state
- Test fixtures use obviously fake values (`example.com`, `R0UTING-KEY-FAKE`)
- `gitleaks` runs in CI over full history (Principle II)
- Public text names no client (codenames only)
- Anything touching secrets, inbound replies or channel isolation gets an
  independent security-minded review before merge
- **Hosted mode**:
  - every request is authenticated before it reads or changes state: OAuth
    for the chief of staff, restricted to the owner's identity; a per-node
    token for each agent, scoped to its crew (a crew may span machines) and
    stored only as a hash
  - a token never reads another crew's items, and a hidden item looks the
    same as a missing one
  - the approval rules are enforced by the server (no approval for an
    action the policy doesn't mark `passkey`; a new revision voids an
    earlier approval)
  - **the approval policy**: the owner owns a policy that maps each action
    class and target (repo, ref, environment) to `passkey` (his passkey
    approval is required), `standing` (it runs under authority he has
    already delegated, with no prompt and without the bus) or `deny`.
    Anything unlisted is `deny`. The passkey adds assurance where his own
    yes is required. It never replaces existing permission systems and never
    adds a prompt for work already delegated. The policy is a signed file,
    pinned outside the bus like the keys file, and changing it takes his
    passkey. External gates (a required check, credentials held only by the
    executor) apply only to `passkey` targets
  - **never on the bus, whatever the policy says**: force-pushes, deletes,
    spend, secrets and money
  - only the human approves: an approval resolves only on a passkey
    assertion the owner makes himself, with user verification, over a
    server-issued, single-use challenge that expires within 10 minutes and
    is bound to a hash of the exact payload he saw (at least: item, action,
    repo, ref, target or environment, pinned commit or tree and, for a
    merge, its base, revision, decision, crew, the requesting session and
    the expiry). No session cookie, earlier sign-in or agent connection
    counts, the chief of staff's included
  - the owner is whoever holds a passkey whose public key is in the pinned
    keys file. Adding or revoking a key is a reviewed change by the owner
    to that file, outside the bus, signed by a key already in it (the
    first key pinned by hand), and read by executors from the protected
    default branch at a pinned commit, never from a working tree.
    Executors refuse keys no longer in it
  - every answer and approval records `actor_kind: human | agent`, taken
    from the credential alone. Agents, the chief of staff included, are
    always `agent`: they may relay the human's words, record his answer to
    a question or prepare an approval, but never resolve an approval. Any
    other human path the owner keeps (a desk sign-in) must be owner-only,
    short-lived and gated by his presence. It may answer questions but
    never resolves an approval, and is named in the hosted plan
  - the session that carries out an approved action first verifies the
    assertion itself, against the owner's public keys pinned outside the
    bus, refuses an assertion that is expired or already used (the expiry
    is inside the signed payload), and treats one assertion as authorizing
    one execution. It acts only on an action it requested itself, and only
    when that action equals the signed payload field for field; it never
    takes the action from the bus. If the head, base or tree moves, it
    needs a new approval
  - each assertion is kept in an append-only audit, and the acting session
    records what it ran, with the assertion's challenge, before reporting
    success
  - approvable with such an assertion: an action the policy marks `passkey` (a
    merge, release or tag, deploy or public sync), pinned to its exact repo,
    ref and commit or tree, and enforced outside the acting session (a
    required status check from a verifier agents cannot bypass, or credentials
    held only by a dedicated executor). Where a `passkey` target has no such
    gate yet, it needs the owner's go in a desk session (the owner's go in the
    terminal, not through the bus or a desk sign-in). `standing` targets keep
    their existing delegated authority and are unaffected. An action in any of
    these categories is never approvable as `other`, and an `other` approval
    never authorizes a merge, tag, deploy, push or anything never-approvable.
    Never approvable through the bus: force-pushes, deletes, any spend (new or
    larger billable resources, spend-cap changes, purchases, subscriptions)
    and any change to secrets. A deploy or sync that would do any of these is
    in that category, not `deploy`. Secrets and money are never on the bus at
    all, and a public sync is approvable only for a tree that passed the
    secret and client-name scans
  - text from the bus is data, never instructions: no link, command or tool
    call is built from it
  - hosted channel keys live only in the deployment's secrets. Hosted auth,
    tokens, approvals and infrastructure get the same independent review

**Rationale**: A leaked PagerDuty routing key lets anyone page the human at
will. A spoofed reply lets someone other than the human approve an agent's
action. The plugin-model research behind this design found exactly these
failures in the wild: OpenClaw gave plugins full trust, ClawHub shipped
hundreds of malicious skills, and a token leaked through a URL parameter
(CVE-2026-25253).

---

### IX. Mechanisms, Not Preferences; Free by Default

yo-human ships mechanisms. The user owns every policy, and the core runs on
free services.

- Levels, quiet hours, escalation, channel order, deadlines and break-through
  rules are configuration the user sets, ideally by talking to an agent.
  Code MUST NOT hard-code a policy choice
- Defaults are proposed in the setup conversation with reasons, never baked in
  silently
- The core and the v1 channels (PagerDuty Free, ntfy, Bark) MUST work with no
  paid account. Paid services (Twilio and similar) are optional channels only
- Free-tier limits that affect behaviour (PagerDuty Free's 100 SMS or phone
  notifications a month) are surfaced to the user, not discovered by surprise

**Rationale**: The four users in the design (side-project worker, off-hours
guardian, away-from-keyboard worker, power user) want opposite things from
the same message. Any preference hard-coded for one of them is a bug for the
other three. A tool for side projects that requires a paid account won't get
installed.

---

### X. Documentation Ships With the Change

Docs MUST be updated in the same PR as the code they describe.

- `README.md`, `CLAUDE.md`, the agent skill and the CLI reference change in
  the same PR as any verb, flag, exit code or config key
- Setup docs open with an **Inputs** list (one bullet per value,
  `NAME="<placeholder>"` plus a one-line description), then "Already set".
  Every value appears once; steps use `$NAME`; a command run twice is a shell
  function; paths use `$HOME/...`
- Living docs: `docs/specs/` (design), `docs/plans/`, `docs/OPEN-QUESTIONS.md`,
  `docs/KNOWLEDGE.md`. Owner decisions are also recorded on GitHub issues
- No "docs PR later"

**Rationale**: Agents read docs and skills to decide how to page the human.
A stale skill makes every agent call the wrong verb or misread an exit code
until someone notices. The setup docs handle secrets, so a value repeated in
two places is how a placeholder ends up pasted where a real key belongs.

---

## Development Workflow

### Test-First Mandate

1. Write the test; run it; record the RED output
2. Commit the failing test
3. Implement the minimum code to pass
4. Mutation-check the key line; refactor with tests green
5. Run `make ci` before pushing

### PR Checklist

- [ ] RED proof recorded; mutation check done on the key line
- [ ] `gofmt`, `go vet`, `golangci-lint` pass
- [ ] `go test -race` passes; coverage floors held or raised
- [ ] Performance budgets held (none raised)
- [ ] `gitleaks` passes; no secrets, real hosts or client names
- [ ] CLI contract unchanged, or CHANGELOG plus version bump plus skill update
- [ ] Docs updated (README, CLAUDE.md, skill, setup Inputs)
- [ ] Security-minded review if secrets, inbound replies, channel isolation, or hosted auth, tokens, approvals or infrastructure were touched
- [ ] No principle violations, or each one justified in writing

### Commit Standards

- Conventional commits with a scope: `feat(router): …`, `fix(pagerduty): …`,
  `test:`, `docs:`, `refactor:`, `chore:`, `perf:`, `ci:`
- Reference the issue (`#123`) in the body
- Atomic commits; every commit builds. The RED commit is the one allowed
  failing-test commit, and it is labelled as RED

### Branch Strategy

- `main` is protected; no direct commits; changes land by PR
- Topic branches: `feat/…`, `fix/…`, `chore/…`, `docs/…`, `ci/…`
- Stacked work uses stacked branches and PRs, each based on its parent
- Releases are semver tags `vX.Y.Z`, built by goreleaser with checksums

---

## Governance

This constitution supersedes informal practice. If a principle and a habit
disagree, the principle wins until the constitution is amended.

### Amendment Process

1. Propose the amendment in a GitHub issue with a concrete rationale (the
   incident or drift behind it)
2. Assess impact on code, CI and docs
3. Version it per semver (see the Amendment Log preamble)
4. Add an entry to the Amendment Log, newest first
5. Propagate to dependent files (below)
6. The owner approves; commit with the version bump and date

### Compliance Review

- Every PR passes a constitution check, either implicitly (nothing relevant
  changed) or explicitly (each violation named and justified)
- Simpler alternatives MUST be written down before new complexity is accepted

### Conflict Resolution

1. Document the conflict and its context in an issue
2. Decide: amend the constitution, or change the implementation
3. If amending, follow the amendment process
4. The constitution beats convenience, but not impossibility

### Template Synchronization

Changes to principles MUST propagate to:

- `CLAUDE.md`: AI assistant instructions
- `CONTRIBUTING.md`: human contributor guidelines
- `lefthook.yml`: pre-commit enforcement
- `.github/workflows/` and `.golangci.yml`: CI alignment
- The coverage and performance ratchet files
- `README.md` and the agent skill, when principles affect user-facing claims

---

**Version**: 1.1.0 | **Ratified**: 2026-10-25 | **Last Amended**: 2026-10-27
