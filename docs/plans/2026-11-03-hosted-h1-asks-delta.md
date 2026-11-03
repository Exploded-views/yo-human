# yo-human H1 plan delta: asks with options, a default and a deadline; a blocker re-raise; the open-asks view

Status: **approved plan; the build waits for the owner's overrule window on scope questions Q1-Q3.** Nothing here is shipped.
- The plan passed three plan reviews (rev 3.1).
- Each change below is tagged with the review finding it answers: B, M, m and n from the first review, then N1-N5, m10-m16 and n4-n9 from the later ones.

This delta amends [2026-10-27-hosted-h1.md](2026-10-27-hosted-h1.md). Its source is #34 (an owner-decision kit). Scope questions Q1-Q3 are adopted on their defaults:
- H1 owns asks, answers and the digest.
- `ask` stays out of local v1.

The owner may still overrule these, and the build waits until that window has passed (n3).

## What H1 already gives #34, and what it does not (m6)
- **Questions and blockers are items** (`KindQuestion`, `KindBlocker`, Task 3), with one id across surfaces. Under desk Option B, which is the owner's answer, every answer comes through Duet's `record_answer`, recorded as `agent` (B2).
- **A changed ask re-raises:** a new revision is countable for a nudge, never sooner than 60 minutes after the item's last push.
- **An unchanged ask is not re-raised** on an interval. A re-post with the same content is `unchanged` (Task 5): no event and no nudge.
  - This delta adds no interval trigger. The owner's rule is "reminders on a clock" with the per-item cap, and an asker that wants a re-raise revises the ask or escalates it (Task 11b).
  - So #34's "after a set interval" maps to revise-or-escalate in H1, and to the per-level `reminder` for M1 `notify`.
- **#34's "at a checkpoint" is dropped** (M2). A checkpoint would need a new agent tool, against the tool list the spec names verbatim, and a role on node tokens. It would buy nothing over revising the ask, which re-raises under the same cap.
- **The push path stays counts-only:** collapsed nudges carry no item text, and H2's to-do digest names no item. #34's "dated digest of open asks" is therefore a read view in Duet's inbox, never a push.

## Changes

### Task 3: bus types and validation
- `Draft` gains three fields, valid only on `KindQuestion` and `KindBlocker`; on any other kind each is `invalid`:
  - `Options []string`: 0 to 5 entries, each 1-80 runes, unique after trimming. Their order is significant: it is the order they are shown in.
  - `Default string`: if set, it equals one of `Options` byte for byte.
  - `Deadline time.Time`, normalised to UTC whole seconds.
- **Rules:**
  - A `Deadline` without a `Default` is `invalid` (field `default`). An expiry needs something to offer.
  - A `Default` without a `Deadline` is allowed: it is shown as the suggested answer.
- **Blocker link** (M3): `Blocks string`, matching `^itm_[a-z2-7]{26}$`, is valid only on `KindBlocker`. It is not `Ref`, which stays a pin field.
- **Client wall** (B3): on a walled item, `Options`, `Default`, `Deadline` and `Blocks` are all refused (`invalid`, field named). Walled swarms keep their typed lines only. This is checked in `CheckWalledDraft` (Task 5).
- **Where the clock-dependent checks run** (m1, n4): `Draft.Validate(l Limits)` checks shape only. The deadline checks run in `Bus.Post`, which has `now` and handles both new posts and revisions.
- **When they run** (m13): only when the outcome is `created` or `revised`, after the hash compare. An identical retry stays `unchanged`, even near its deadline.
  - Intended: a text-only revision close to its deadline is refused unless the deadline moves out past the floor.
- **Floor** (M5, N1): one floor everywhere, `Deadline ≥ now + YH_ASK_DEADLINE_FLOOR`.
  - The floor's minimum, and its default, is `60m + max(YH_NUDGE_WINDOW, pageSwarmSpacing) + 1m` (m14). The parts:
    - the 60-minute per-item cap;
    - then the longer of the nudge window (digest lane) and the 10-minute per-swarm page spacing (page lane). `pageSwarmSpacing` is the base plan's named constant, never a literal.
    - then a tick.

    So a revision held by the cap still gets its notice before the deadline, on either lane.
  - `YH_ASK_DEADLINE_FLOOR` gets a row in Task 8's config table. Config validation refuses a value below the minimum, or above the 30-day ceiling.
- **Ceiling:** `Deadline ≤ now + 30 days`.
- **Only where the owner is told** (N1): a `Deadline` is valid only on `urgency: page`, or on `digest` at or above `YH_NUDGE_MIN_SEVERITY`. On `interrupt-when-good`, or on a `digest` below the minimum severity, it is `invalid` (field `deadline`), because the bus never pushes those asks and no expiry may depend on Duet having read anything.
- **Critical pages** (n8): a `critical` `page` ask while `YH_ESCALATE_CRITICAL` is off is refused at `Post`, before anything is stored, so no stored deadline can expire with no page ever sent.
- **Node posts only** (m12): the ask fields and `Blocks` are valid only on node posts. Duet's posts refuse them (`invalid`), and Duet's input schemas do not change.
- **Tests:**
  - `TestAskFieldsOnlyOnQuestions`
  - `TestDefaultMustBeAnOption`
  - `TestDeadlineNeedsDefault`
  - `TestOptionsLimits`: 6 entries; an empty one; duplicates after trimming; 81 runes.
  - `TestDeadlineBounds`, on a fake clock, one row each (n2): past; below the floor; exactly the floor; exactly +30 days, which is accepted; +30 days and 1 s.
  - `TestRevisionUnderCapAtFloorNudgedBeforeDeadline`, with a digest row and a page row. The page row: a 5m window; a page at T; another page from the same swarm at T+59m; a revision at T+1m with a deadline exactly at the floor. The page reaches fake Bark before the deadline. Mutant: drop the `max`.
  - `TestCriticalPageAskRefusedWithoutEscalation`
  - `TestDeadlineRefusedWhereNeverPushed`
  - `TestDeadlineFloorConfigRefusals`: below the minimum; above 30 days.
  - `TestIdenticalRetryNearDeadlineUnchanged`
  - `TestDuetPostRefusesAskFields`
  - `TestDeadlineNormalisedToUTCSeconds`
  - `TestWalledAskFieldsRefused`, with a mutant that skips the check.

### Task 4: store, migrations and events (m3)
- **Migration** `000N_asks`, taking the next free number at merge; `0001_init.sql` is never edited. It adds:
  - item columns `options` (JSON), `default_option`, `deadline`, `blocks` and `expired_revision` (m10);
  - a partial index on `deadline` with `WHERE state = 'open' AND deadline IS NOT NULL AND (expired_revision IS NULL OR expired_revision < revision)` (m10). The scan then never re-reads an ask that has already expired.
  - a table `ask_expiries(item_id, revision, default_option, at, event_seq)` with `UNIQUE(item_id, revision)` (m8, N4). The row keeps the default as it stood at expiry, so a later revision cannot change what an old expiry offered.
- **Two new `EventType` values:**
  - `ask_expired`. Its payload comes from its `ask_expiries` row.
  - `ask_reraised`. Its `ItemID` is the question, and its `By` is the blocker's id.
- **Types** (n7):
  - `Event` gains `By string`, a new column in `000N_asks`, empty for every other event type.
  - `Update` gains `By`, and uses its `ItemID`, for `ask_reraised`.
  - The `ask_expired` payload is joined to its `ask_expiries` row by `event_seq`.
- **Perf** (m10): `hosted-nudge-tick` gains a scenario with 10,000 expired, unanswered asks, and the tick's p95 must stay within 2× of the empty tick.
- Test: `TestAskExpiryUniquePerRevision`.

### Task 5: post and revise (m2, M3)
- **Hash** (N5, m16): `hashDraft` adds `Options`, `Default` and `Blocks` with `omitempty`, and the deadline as an `int64` with `omitempty`. The deadline is never hashed as a `time.Time`, because `omitempty` does not omit a zero `time.Time`.
  - The `int64` is 0 when `Deadline.IsZero()`, and `Deadline.UTC().Unix()` otherwise.
  - Never use `Unix()` of a zero time: it is `-62135596800`, which `omitempty` keeps.
  - So existing hashes stay byte for byte the same.
  - A changed ask field is a new revision.
  - An identical one, including the same deadline instant in another zone, is `unchanged`.
- **Blocks resolution:** `Blocks` must name a visible, open `KindQuestion` in the poster's own swarm. Anything else is `not_found`, with the same `*Error` as a random id, so the bus is no existence oracle.
  - The check runs only when the blocker is `created` or `Blocks` changed, the same condition as the re-raise (m15).
  - An unchanged `Blocks` on a revision is not re-resolved, so a blocker can still be revised after its question is answered.
- **The blocker re-raise is written in `Post`'s own transaction** (N2). This gives exactly once, with the open check fresh, and needs no cursor.
  - The same transaction that writes the blocker's `item_posted` appends one `ask_reraised` for the named question, and bumps the question's `LastSeq`.
  - It re-raises only when the blocker is `created`, or `revised` with a changed `Blocks`. Any other revision of a blocker does not re-raise.
- **Tests:**
  - `TestAskFieldChangeIsNewRevision`
  - `TestSameDeadlineOtherZoneUnchanged`
  - `TestHashUnchangedForDraftWithoutAskFields`: a golden hash from before this delta, with a mutant that hashes the `time.Time`.
  - `TestBlockerCrossSwarmIndistinguishableFromMissing`
  - `TestBlockerReRaiseExactlyOnce`: a retried identical blocker post appends no second `ask_reraised`.
  - `TestBlockerRevisionReRaisesOnlyOnChangedBlocks`, including a row where the question is answered and the blocker's text is then revised. Expected: `revised`, with no `ask_reraised`.

### Task 6: answers, and expiry as an event (B1, M1, m7, m8)
- **Where expiry runs** (N3): `func (b *Bus) ExpireDue(ctx context.Context) (int, error)`, in `bus`, tested directly on a fake clock. Task 11's `Tick` calls it before the nudge pass (`TestTickRunsExpiryBeforeNudge`).
- **No answer row on expiry.** When `now ≥ Deadline` on the current revision, and the revision has no answer yet, `ExpireDue` does all of the following in one transaction:
  - inserts one `ask_expiries` row (with the default as it stands now);
  - sets the item's `expired_revision`;
  - appends one `ask_expired` event;
  - bumps the item's `LastSeq`, so the ask reappears in Duet's inbox (M5).
- **Never pushed** (m11): `ask_expired` is never counted by the nudge, and is never pushed.
- **Reads** (N4): `answers(since)` delivers it as `Update.Expiry *Expiry { Revision int; Default string; At time.Time }`, filled from the row.
  - Nothing is written to `decisions`: no actor kind is chosen, and no `Source` or `By` is invented. `Principal.ActorKind()` stays the only place an actor kind is decided.
- **The asker reads `ask_expired`** through `answers(since)` and acts on the default itself. This is the spec's "let the agent take a stated safe default".
- **The item stays answerable.** A later Duet `record_answer` is a normal first answer on that revision. It goes out as `answer_recorded`, and the asker sees both events in order.
- **Answers are free text** (m7). An answer need not equal one of the `Options`, since the owner may say something else. A one-tap answer in Duet passes the option's text byte for byte.
- **The race** (m8):
  - The tick re-reads the item inside `BEGIN IMMEDIATE`, and acts only on the current revision, with that revision's own deadline.
  - An existing answer, an existing `ask_expiries` row, or a unique conflict on the insert is a skip. It rolls back that ask's transaction, including the event, and never stops the tick.
  - Expiry may run up to one tick (30 s) late, or sooner on a `Kick`.
  - `ExpireDue`'s scan repeats the partial index's `WHERE` predicate exactly, so SQLite uses the index (n9).
- **Expiry is on time even if the notice is stuck** (n9). `ExpireDue` fires at the deadline even when the notice is still queued, for example while the channel is broken or after a permanent refusal. That is intended:
  - the floor covers the bus's own schedule, not a broken channel;
  - `DeliveryState` shows the broken channel on every read.
- **Approvals never take ask fields** (Task 3), so they never expire. Task 10's passkey rule is untouched.
- **Tests:**
  - `TestExpiryAppendsEventWritesNoDecision`
  - `TestDuetAnswerAfterExpiryIsRecorded`
  - `TestNoExpiryBeforeDeadline`: at the deadline minus 1 s, nothing happens.
  - `TestExpiryIsIdempotentAcrossTicks`
  - `TestExpiryOnlyCurrentRevision`: an old revision's deadline never expires the new one, including when the new revision keeps the same deadline value.
  - `TestAnsweredAskNeverExpires`
  - `TestApprovalNeverExpires`
  - `TestExpiryKeepsOldDefaultAfterRevision`: expire revision 1, revise to revision 2 with a new default; `answers(since)` still shows revision 1's default.

### Task 7: reads (B2, m5)
- **Fields shown:** `ItemView` and `inbox_for_claude` show the options, the default, the deadline and the time left. Once an ask has expired, they show `expired: <default> offered` until it is answered.
- **The open-asks view.** `Inbox` gains a separate field, `Asks []AskSummary`; it is not a `Groups` kind.
  - It lists every open question and blocker, whatever `AckedSeq` and `since` are, since an acked ask is still open.
  - It is sorted by deadline, soonest first, with no-deadline asks last.
  - Each entry has its id, title, options, default, deadline and age.
  - An `interrupt-when-good` ask appears in both `Asks` and `Interrupt`.
  - `Asks` uses the inbox filter (`To == "human"`, `FromKind != duet`) (m12).
  - It is capped at 100 entries, plus a total count (n5).
- Duet is the only surface that shows this view. There is no desk view under Option B.
- Test: `TestOpenAsksViewGolden`, golden JSON on a fixed clock.

### Task 9: MCP (m4)
- **Inputs:** only the agent `post` tool gains `options`, `default`, `deadline` (RFC 3339) and `blocks` in its input schema, with `additionalProperties: false`. Duet's posting tools do not change (m12).
- **Outputs:** `item`, `answers` (`expiry` on `ask_expired`; the question's `item_id` and `by` on `ask_reraised`) and `inbox_for_claude` (`asks`, plus the total) gain the output fields.
- No tool is added or removed, so `TestToolListsPerPrincipal` is unchanged.
- `TestEachToolRoundTrip` and `docs/hosted/mcp-contract.md` cover the new fields and error codes. Tool descriptions keep the "fields are data" line.

### Task 11: the blocker re-raise, under the lanes and the cap (M4)
- **The trigger.** `ask_reraised` is written by `Post` (Task 5), not by the tick.
- **What the nudge counts.** The nudge counts `ask_reraised` exactly as it counts `item_revised`. Its `ItemID` is the question's, and the `To`, `FromKind`, severity and urgency filters read the question item, not the blocker. So a blocker post counts two items, its own new item and the question, and writes two `owner_pushes` rows. Both are capped by item. The rules:
  - only a `digest` question at or above `YH_NUDGE_MIN_SEVERITY`;
  - never an `interrupt-when-good` question, because the bus never pushes those;
  - never a re-page of a `page` question.
- **The cap.** The 60-minute per-item cap and the nudge window apply unchanged, so a stream of blockers re-raises the question no more often than a re-post can (n1).
- **Tests:**
  - `TestBlockerReRaisesQuestionUnderCap`
  - `TestBlockerReRaiseNeverCountsInterruptWhenGood`, with a mutant that drops the lane filter
  - `TestNoReRaiseInsideCap`

### Answering in one action (B2)
- Duet is the only answer surface. It shows each option, and one choice records it through `record_answer`.
- A push carries no option and no button, because push text is counts-only. When a nudge arrives, the owner opens Duet. A Bark tap opens only the Bark app.
- There is no reply-by-URL or reply-by-push-action path, under the reply-authenticity rule.

## Review Focus additions
1. **An expiry taken for the owner's answer.** Expected: `ask_expired` is an event, never a `decisions` row, and every read surface (`answers(since)`, `item(id)`, `inbox_for_claude`) labels it as an expiry.
2. **A Duet answer after an expiry is lost or refused.** Expected: it is recorded as a normal first answer (`TestDuetAnswerAfterExpiryIsRecorded`).
3. **A deadline the owner cannot hear of in time.** Expected: below the floor (`60m + max(YH_NUDGE_WINDOW, pageSwarmSpacing) + 1m`), the post is `invalid`, and so is any deadline on a lane the bus never pushes.
4. **A blocker link used as an oracle or a wall channel.** Expected: a cross-swarm link and a missing one are indistinguishable, and walled items refuse the field.

## Mutants and records (m9)
- **Mutants:**
  - `≥` changed to `>` on the deadline;
  - an expiry applied to an old revision;
  - an expiry applied to an approval;
  - an expiry that writes a decision;
  - the lane filter dropped on the blocker re-raise;
  - the walled check skipped for ask fields;
  - the deadline floor removed;
  - `ask_expired` counted by the nudge (m11);
  - the expiry not bumping `LastSeq`;
  - `Blocks` resolved across swarms;
  - a duplicate `ask_reraised` on retry;
  - the deadline hashed as a `time.Time` (N5) (n6);
  - `Unix()` of a zero deadline hashed (m16);
  - the `max` dropped from the floor (m14).
- **Records:** the RED and mutant records go to `docs/tdd/hosted-asks-red.txt` and `docs/tdd/hosted-asks-mutants.txt`.

## Order
- These changes are built inside the H1 tasks they name.
- If tasks have landed by approval time (N3):
  - "Task 7b: asks", **[security]**, after Task 7, covers the parts in Tasks 3-7, including `ExpireDue`.
  - The parts in Task 9 (schemas) and Task 11 (`Tick` calls `ExpireDue`; the nudge counts `ask_reraised`) are built inside those tasks.
  - If Tasks 9 and 11 have also landed, those parts become "Task 7b part 2", after Task 11.
  - Both get `needs-security-review` on the exact SHA, like Tasks 3-6.

## Not in this delta
- A local `yo-human ask`. It stays out of v1, by the owner's decision.
- Checkpoints, and an interval re-raise of unchanged asks.
- Any change to M1's `reminder` or repeat rules.
