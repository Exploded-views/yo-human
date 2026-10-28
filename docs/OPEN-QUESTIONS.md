# Open questions for the owner

**All eight answered by the owner on 2026-10-25.** Each question keeps its original suggestion for the record; the owner's answer follows it as **Decided**.

From an independent "builder check" of the design (2026-10-26): places where a builder would have to guess and a wrong guess changes the plan. Most important first. Each has a suggested answer that is ONLY a suggestion; the owner decides. Record the answer here and in the design doc's "Decided by the owner" section.

1. **What is v1, and in what order does the rest arrive?** Two first releases fit the design: "off-hours guardian" (outbound notify, PagerDuty critical, silence otherwise) or "away-from-keyboard worker" (two-way `ask`, question sessions, answers via Heeler).
   - Suggested: v1 = notify plus ask: outbound notify through PagerDuty and ntfy, plus `ask` with question sessions on today's Heeler. That serves the owner's own main use (being away from the keyboard) and covers the guardian's loud page too.
   - **Decided:** v1 = the off-hours guardian: outbound notify through PagerDuty, ntfy and Bark, with dedup, resolve, quiet hours and the Q7 shim. `ask` gets built, but after v1.

2. **Are question sessions committed for v1?** If yes, herdr is a core dependency and answers come back through a herdr session. If no, replies must come from channels themselves (ntfy actions, SMS reply codes, Heeler notification replies), which leans on upstream Heeler work.
   - Suggested: yes for v1, behind a channel-neutral question store, so channel-native replies can be added later without a redesign.
   - **Decided:** no question sessions in v1, so herdr is not a v1 dependency. When `ask` comes, it sits behind a channel-neutral question store as suggested. Until then, remote answers go through Heeler's existing Blocked-session flow.

3. **Must it work for people who don't run herdr and Heeler?**
   - Suggested: the core and the outbound channels work without them. The full two-way experience is best with herdr plus Heeler, and other reply paths come later as channels.
   - **Decided:** as suggested. The core and outbound channels work without herdr or Heeler.

4. **Where does it run, and how do replies get in?** One always-on host or a sleeping laptop? Agents on several hosts? Is a public inbound endpoint (a tunnel) OK, or must every inbound channel poll outbound?
   - Suggested: v1 runs on one always-on host next to the agents; agents on other hosts call it over SSH or a small authenticated API later. Inbound is polling or outbound-only where possible (PagerDuty REST incident state, Heeler via herdr over SSH); a tunnel is optional, never required. Durable local queue, so nothing is lost if delivery is down.
   - **Decided:** call-only, no daemon. yo-human is a utility agents run with arguments; it sends inline under a hard, user-configurable deadline (default 1 s), queues locally on timeout or failure, and exits with a distinct "queued" code. Every later call (or `yo-human flush`, optionally from cron) sends what's queued. Timed behaviour (re-paging, delayed delivery) is left to the services (PagerDuty escalation, ntfy scheduled delivery). Agents on other hosts use `ssh <host> yo-human ...`; no inbound endpoint or tunnel.

5. **Does v1 wait on upstream Heeler changes?**
   - Suggested: no. v1 uses only Heeler's current surface (agent status → push; chat answers in a Blocked session). Upstream PRs (custom messages and levels, time-sensitive, notification actions, reply from notification) run as a parallel track, and so does the fork-sync Action.
   - **Decided:** as suggested. v1 doesn't wait on upstream Heeler changes.
   - **Update (2026-10-27, constitution v1.0.2):** Heeler is dropped from v1. The design moved beyond it; it stays a candidate later channel for a human reaching agents directly while on the move. The reference is [the Heeler research](research/2026-10-26-heeler.md).

6. **One human per install, or teams and rotations?**
   - Suggested: one human for v1, with identity modelled so that more people (rotations, escalate-to-person) can be added later without a rewrite.
   - **Decided:** as suggested. One human, with a `human` id on every message from day one.

7. **How does it relate to agents that already page PagerDuty directly?**
   - Suggested: yo-human becomes the one way agents reach the human (dedup, quiet hours and "never page twice" only mean something then). Ship a drop-in shim for an existing page script, so migration is one line per caller.
   - **Decided:** as suggested. yo-human is the only way agents page the human, with a drop-in shim that keeps the current script's flags, including `--resolve <dedup_key>`.

8. **Which agent runtimes at launch?**
   - Suggested: Claude Code first (skill plus MCP), with the CLI as the runtime-neutral contract every other agent can use. Answers come back through the CLI (`yo-human wait <question-id>` with bitc-style exit codes) so nothing depends on one runtime's messaging.
   - **Decided:** Claude Code skill plus the CLI; no MCP server in v1 (add one later only if a runtime needs it). Language: Go.
