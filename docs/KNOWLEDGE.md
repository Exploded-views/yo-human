# Knowledge dump: what the next session should know

Written 2026-10-26 by the session that ran the original design conversation, for a fresh session on the owner's laptop. Non-sensitive by design: no keys, hosts, vault IDs, emails or client details. If something here conflicts with the design doc or OPEN-QUESTIONS.md, those win.

## How the owner works (apply to everything)

- **Strict test-first.** The failing test is committed first with its RED proof recorded, then the fix, then a mutation check on the key line. Reviews check the RED proof.
- **Ratchets in CI:** a coverage floor per package that only rises, and performance budgets that only tighten (p95 over N runs, a recorded headroom factor, process counts asserted exactly where they're deterministic).
- **Performance matters.** Hot paths take milliseconds: no network or decryption on an agent's critical path; fail silent with exit 0 rather than block. Fix slow hooks properly; don't raise timeouts.
- **Docs ship with every feature.** Setup docs open with an **Inputs** list (one bullet per value, `NAME="<placeholder>"` plus a one-line description), then "Already set". Every value appears once (**DRY**); steps use `$NAME`; a command run twice is a shell function. Use `$HOME/...`, not a quoted `~`.
- **Mechanisms, not preferences.** Anything that is a user preference is configurable, set by the user (ideally conversationally through an agent), never hard-coded.
- **Don't spend tokens on risks that won't occur.** Weigh likelihood against cost; note an unlikely risk in one line and move on, or ask.
- **Free by default.** Paid services are optional plugins.
- **GitHub hygiene:** GitHub-hosted runners only (never self-hosted in this org); never `pull_request_target`; pin every action to a full commit SHA; fork-PR approval for all outside collaborators on public repos; owner decisions recorded on GitHub issues.
- **Never print secrets.** Checks report names and counts only. Credentials are piped, never pasted: `op read "op://<vault>/<item>/<field>" | ssh <host> '<consumer>'`. 1Password field names vary (`credential` for API Credential items, `token` for items auto-saved from a browser); check the byte count first.
- **Inputs first in anything handed to the owner, and DRY.** Present decisions with a recommendation; he decides.

## The crewkit family

- **crewkit:** the toolkit: plugins (agent-practice, memory-notices), skills (herdr, using-bitc, using-memsync), a tested `install.sh` that pins releases by tag plus sha256, a workflow lint (`tools/check-workflows.py`), perf budgets, inputs-first docs lint, before-and-after skill evals (RED without the plugin, GREEN with it, identical flags, a fake memory index).
- **bitc:** runs heavy commands through a slot pool. Contract: the command's own exit code when it ran; **75** = starved or transient, retry the same command a bounded number of times then report STARVED; **124** = `bitc wait` still running, wait again; **69** = unavailable, stop and report; **64** = usage or refused, fix the call; **66** = no such job; **78** = invalid config. Detached jobs: `bitc run --detach --name <job> <pool> -- cmd`, then `bitc wait <job>`, whose first line is `RESULT exit <code>` or `RESULT starved`. yo-human's CLI should follow the same style: stable documented exit codes, a skill that teaches them.
- **memsync:** syncs Claude Code memory across machines through private git repos with post-quantum encryption. Its per-prompt hook taught the "self-deadline, fail silent, never mark notices seen on overrun" lesson.
- **lanes, orchestrator and watch roles** are being generalized into crewkit plugins (crew-lanes, crew-orchestrator, crew-watch). yo-human is milestone 4 of that plan, now promoted to its own repo.

## herdr (what yo-human will use)

- Read state without interrupting: `herdr agent list`, `herdr agent get <name|pane>`, `herdr agent read <name|pane> --source recent-unwrapped --lines 120`. Status values: working, idle, done, blocked, unknown (unknown proves nothing).
- Spawn a named session without stealing focus, from an already-trusted folder (or it stops at the trust dialog):
  `herdr pane split --current --direction down --cwd "<trusted dir>" --no-focus` (read the pane id from `.result.pane.pane_id`), then
  `herdr agent start <name> --kind claude --pane <pane-id> -- --name <name> --permission-mode <mode>`, then brief it with `herdr agent prompt <name> "<brief>" --wait --timeout 120000`.
- **Never** `agent prompt`, `send-keys` or `pane run` into a session you didn't start: it arrives as the owner's own input. Never answer another session's Blocked dialog through herdr.
- `herdr notification show <title> [--body] [--sound none|done|request]` shows a local notification. `herdr api snapshot` and `herdr api schema` expose the socket API. herdr plugins subscribe to events like `pane.agent_status_changed` (that's how Heeler's plugin works).
- Claude Code sessions talk to each other with SendMessage; herdr is for looking, spawning and screens.

## Paging today (what yo-human replaces)

- A ~30-line script posts to PagerDuty Events API v2 with two services: **critical** ("need human now": production down or impaired) and **low** ("CI/CD broken while prod is up"). Same `dedup_key` = same incident; `--resolve <dedup_key>` resolves. Everything else goes to ntfy. Routing keys live in local files, never in code.
- Agents had no shared rules for when to page beyond notes, and no way to read an acknowledgement back. Real cases from one day: a release going live (FYI), a bug blocking all releases (needs attention, not critical), the owner needing to run a command only he could run (needs-you, two-way), a peer session blocked on the owner's push (needs-you, low urgency), a production health check failing twice (critical).

## Claude Code hooks lessons

- UserPromptSubmit and SessionStart hooks have a timeout (10 s here). On timeout, Claude Code discards the output, but the hook process may still finish, so a hook that "marks seen" can silently lose notices. Give hooks a self-deadline (about 1.5 s) and make them move no state when they overrun.
- Host IO contention (many sessions waking at once after a usage reset) can make a normally 0.3 s hook take over 10 s. Budget for it.

## Heeler and the fork

- Heeler details, gaps and our plan: `docs/research/2026-10-26-heeler.md`.
- Our contribution fork: a planned fork of Heeler. The owner can run Claude Code's `/install-github-app` on it to provision the Claude GitHub App plus a starter workflow, which we then edit into the guarded daily sync job (Claude summarizes upstream in an issue and opens a sync PR; it never merges).

## Naming

The owner picked **yo-human** (product and CLI): robots looking for their human. App Store-style subtitle idea: "Middle management for your AI". Alternatives considered: Mgmt, Hooman, Psst, Skip Level (and ~100 more). Lesson from top app names: one short real word or a light coinage; the joke goes in the subtitle.

## Pitfalls already hit (don't repeat)

- Piping a check through `tail` hides its exit code: check exit codes directly.
- bats ignores `! cmd` except on a test's last line; use a helper that fails explicitly.
- A mutation that is a syntax error isn't a "killed" mutant; reject mutants that don't parse.
- A plugin that emits only "allow" rules may enforce nothing; deny-by-default must be explicit and verified after it's applied (a real bug found in a sandbox egress policy).
- Long transcripts and many long-lived sessions are a large share of disk IO on a small host; offload heavy work.
