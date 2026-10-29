# Skill evals: RED (no skill) and GREEN (skill), side by side

Task 13 (#39). The skill is `skills/yo-human/SKILL.md`; the cases are `evals/cases.json`
(12 cases at pass 1; 16 from fix round 1, each with `must` and `must_not` items a grader checks from the transcript alone).

- Date: 2026-10-26
- Model: `claude -p --model sonnet` (the CLI's sonnet alias), for the runs and the grader
- CLI: Claude Code 2.1.310
- Skill versions: pass 1 ran skill sha256 `c0ef2ecda00b` (first draft); pass 2 ran the committed `9bb79134d391` (the one change: the exit 69 row now says "Retrying cannot help"). Binary and spec: yo-human built from this branch; the skill text follows the binary, see the report for the differences
- Runner: `python3 -I tools/run_evals.py` (adapted from crewkit, see `tools/evals.sha256` and `evals/README.md`)
- Both arms: `--no-session-persistence --setting-sources "" --strict-mcp-config --tools Skill`, stream-json, an empty run directory. Only the skill arm loads `skills/yo-human`. Each case is a dry run: the agent says what it would run; nothing executes (no Bash tool).
- Grading: a second `claude -p` call with no tools, given only the case and the redacted transcript, returning a verdict per item. A case passes only if every `must` is true and no `must_not` is violated. One grader pass per transcript; the grades are the model's judgement, not hand-edited.
- Transcripts are redacted by the runner (ULIDs, timestamps, session ids, key-like strings) and checked with `evals/leakcheck.sh`. They are in `docs/tdd/skill-evals/<arm>-pass<N>/` with `<case>.txt` (reply) and `<case>.grade.json`.

## Results

| Case | RED (no skill) | GREEN (skill-pass1) | GREEN (skill-pass2) |
|---|---|---|---|
| `level-from-levels` | FAIL | PASS | FAIL |
| `level-picks-by-use-when` | FAIL | PASS | PASS |
| `always-ask` | FAIL | PASS | PASS |
| `dedup-key` | FAIL | PASS | PASS |
| `resolve-when-fixed` | FAIL | PASS | PASS |
| `exit-75-follow-next` | PASS | PASS | PASS |
| `exit-69-stop` | FAIL | FAIL | FAIL |
| `exit-78-74-stop` | FAIL | PASS | PASS |
| `exit-64-fix-call` | PASS | PASS | PASS |
| `no-secrets-in-args` | FAIL | PASS | PASS |
| `page-shim-migrated` | PASS | PASS | PASS |
| `tool-not-decider` | PASS | PASS | PASS |
| **Total** | 4/12 | 11/12 | 10/12 |

### RED misses, per case (grader notes)

- `level-from-levels`: Reply runs levels first and picks the level from its output, but never shows a notify command with --level, --title and --ask (it only describes the send generically).
- `level-picks-by-use-when`: Command uses --level siren and a non-empty --ask, but has no --title flag (message is positional), so the title requirement is unmet.
- `always-ask`: The reply uses nudge and --ask with a concrete sentence, but its example command has no --title (the body is passed positionally) and the syntax is hedged as a guess.
- `dedup-key`: The reply's command has no --dedup-key and no --ask flag, and it handles repeats with a local marker file instead of reusing a key, so it sends with no dedup key; it does use valid level names only.
- `resolve-when-fixed`: The reply sends a new murmur notify with a different dedup key (disk-full-build-FAKE-resolved) instead of running `yo-human resolve --dedup-key disk-full-build-FAKE`, and no note is given.
- `exit-69-stop`: Reply stops notifying and reports to the human in the final output with problem and ask, but only says retrying wouldn't fix credentials while also conditionally allowing a retry if transient, so 'retrying cannot help' is not cleanly stated; it does not retry or change levels, though running diagnostics is read-only and it explicitly avoids editing config.
- `exit-78-74-stop`: The reply does not stop and tell the human in-session; it tries a fallback nudge and, for exit 74, retries the notify command, so it fails the stop/no-retry requirements, though it does not run secret set or edit config and it reports the failure honestly in its final output.
- `no-secrets-in-args`: The reply gives a tentative command with siren, --message (not --title) and --ask, uses a placeholder log path rather than naming a real file, and does not leak the token, but it hedges on syntax and never commits to an exact command with a title.

### Remaining misses, skill-pass1

- `exit-69-stop`: Reply stops using yo-human and tells the human the problem and ask, but it never states that retrying cannot help; it only says it won't retry and that the failure is permanent, which is close but not explicit about retry futility.

### Remaining misses, skill-pass2

- `level-from-levels`: It runs levels --json first and the notify has --level, --title and --ask, but it only uses a placeholder <urgent-level> and never actually reads or selects a real name from output (it is a dry run), so the level-from-output criterion is only weakly met.
- `exit-69-stop`: Reply stops using yo-human and tells the human with problem and ask, but never states that retrying cannot help; it only says the skill forbids retrying.

## Reading the results

- RED: 4 of 12 pass without the skill. The misses are the contract the model cannot know: `--title` and `--dedup-key` flag names (it invents `--message` or a positional), `resolve` (it sends a "resolved" notify under a new key), the stop-and-tell rule for 78/74, and secrets. Cases that rest on general sense pass (exit 75 with `next` on screen, exit 64 with the fix line on screen, `page` in the old shape, and not notifying when its rules say not to).
- GREEN: 11 of 12 on pass 1 and 10 of 12 on pass 2. These are single samples per case, graded by one model call, so a one-case swing is noise, not a regression.
- `exit-69-stop` fails in both GREEN passes on one item, "says retrying cannot help". The replies stop, do not retry, switch level or touch config, and tell the human with the problem and the ask; they say the skill forbids retrying but not in those words. Pass 2 ran after the skill row was reworded to say "Retrying cannot help"; the reply still did not echo it. Treat this as a strict `must` item, not a behavioural miss; it is left as the grader judged it.
- `level-from-levels` failed on pass 2 only. Its `must` item "chooses the level name from the levels output" cannot be met in a dry run where the agent has not run the command, so it has to write a placeholder. The pass 1 grader accepted the placeholder, the pass 2 grader did not. The case wording is a known weakness; it was not changed after the runs, so the numbers stay comparable with RED.
- Three full passes were run (RED, GREEN 1, GREEN 2); no more.
- `exit-64-fix-call` and `tool-not-decider` did not load the skill on pass 1 (`skills used: []`) and passed anyway; the skill arm only matters where the model has to know the contract.

## Fix round 1 (pass 3)

The review found that the skill told agents to repeat `flush` while it exits 75, but `flush` always exited 0 with `RESULT ok`, and that a repeat after a dead-lettered first send was suppressed. Both are fixed in the binary (`docs/tdd/skill-fix1-red.txt`, `skill-fix1-mutants.txt`); the skill now describes the real `flush` contract and says to run `next.do` as printed. Four flush cases were added, `exit-69-stop` and `level-from-levels` were reworded (behaviour, not a literal phrase), and the grader no longer sees the `skills used:` header (pass 1 and 2 grades were made with it visible). The numbers of passes 1 and 2 are kept as they were; their case sets differ, so `-` means the case did not exist then.

Pass 3 ran skill sha256 `6342b459c20b` (116 lines), CLI 2.1.310, sonnet alias, 2026-10-26, same flags for both arms.

| Case | RED pass 1 | GREEN pass 1 | GREEN pass 2 | RED pass 2 | GREEN pass 3 |
|---|---|---|---|---|---|
| `level-from-levels` | FAIL | PASS | FAIL | FAIL | PASS |
| `level-picks-by-use-when` | FAIL | PASS | PASS | FAIL | PASS |
| `always-ask` | FAIL | PASS | PASS | FAIL | PASS |
| `dedup-key` | FAIL | PASS | PASS | FAIL | PASS |
| `resolve-when-fixed` | FAIL | PASS | PASS | FAIL | PASS |
| `exit-75-follow-next` | PASS | PASS | PASS | PASS | PASS |
| `flush-still-queued` | - | - | - | PASS | PASS |
| `flush-dead` | - | - | - | PASS | PASS |
| `flush-delivered` | - | - | - | FAIL | PASS |
| `flush-exit-0-nothing-sent` | - | - | - | FAIL | PASS |
| `exit-69-stop` | FAIL | FAIL | FAIL | FAIL | PASS |
| `exit-78-74-stop` | FAIL | PASS | PASS | FAIL | PASS |
| `exit-64-fix-call` | PASS | PASS | PASS | PASS | PASS |
| `no-secrets-in-args` | FAIL | PASS | PASS | FAIL | PASS |
| `page-shim-migrated` | PASS | PASS | PASS | PASS | PASS |
| `tool-not-decider` | PASS | PASS | PASS | PASS | PASS |
| **Total** | 4/12 | 11/12 | 10/12 | 6/16 | 16/16 |

- RED pass 2 is 6 of 16: it misses the flags, `resolve`, the 78/74 and 69 stops, secrets, and both new flush cases that depend on reading the result (`flush-delivered`: it keeps flushing or tells the human it failed; `flush-exit-0-nothing-sent`: it calls the human reached). It passes `flush-still-queued` and `flush-dead`, where general sense is enough.
- GREEN pass 3 is 16 of 16, from one sample per case and one grader call each. Treat it as "no miss seen", not as proof; pass 2 of the older set showed one-case swings from grader noise. Three replies (`flush-delivered`, `flush-still-queued`, `tool-not-decider`) did not load the skill and passed anyway.
- The grader was changed to take the first JSON object in its reply (it crashed on trailing text once) and to resume past grades already made; no grade was edited by hand.

## Fix round 2 (targeted rerun of three flush cases: guards, not part of the RED/GREEN delta)

The skill text changed (flush exit 0 means nothing failed and nothing is left to retry in this run; "no lines" means nothing was due yet; `suppressed` means an earlier page is pending or delivered), and the binary's `next` now reports the real retry. Only `flush-still-queued`, `flush-dead` and `flush-delivered` were rerun, both arms, skill sha256 `4f0cc3a03063`, same flags, blind grading. No full pass.

| Case | RED (no skill) | GREEN (skill) |
|---|---|---|
| `flush-still-queued` | PASS | FAIL |
| `flush-dead` | PASS | PASS |
| `flush-delivered` | PASS | PASS |

- The skill arm failed `flush-still-queued` on one grader item: the reply ends with "run flush again only if it seemed useful", read as an unbounded retry. It is recorded as graded; the reply is in `skill-evals/skill-pass4/`.
- These three cases do not separate the arms: the base model passes all three when the canned output is on screen. They guard against the skill making things worse, and the discriminating flush cases are `flush-delivered` and `flush-exit-0-nothing-sent` in pass 3 (not rerun here).
- Results for earlier passes are unchanged.

## Fix round 3: guards, and one more rerun

The three flush cases (`flush-still-queued`, `flush-dead`, `flush-delivered`) are **guards**: the base model passes them with the canned output on screen, so they measure whether the skill makes things worse, not what it adds. They are reported here apart from the RED/GREEN delta, which stays in the pass 1 to 3 table above (`flush-delivered` and `flush-exit-0-nothing-sent` in pass 3 did separate the arms, 2 of 2 fail without the skill).

| Guard | base | skill |
|---|---|---|
| `flush-still-queued` (round 2) | PASS | FAIL |
| `flush-dead` (round 2) | PASS | PASS |
| `flush-delivered` (round 2) | PASS | PASS |
| `flush-still-queued` (round 3: context updated to the real flush output, "120s, 4 attempts left"; skill sha256 `11b7f74097dc`, bounded-retry wording rewritten) | PASS | FAIL |

In round 3 the skill arm did not load the skill (`skills used: []`), so that reply is effectively a base reply, and the grader failed it for an unbounded loop ("for as long as it gives attempts"). So the FAIL says the guard case is sensitive to whether the skill is loaded, not that the new wording failed. It is recorded as graded and not rerun (one targeted rerun was allowed).

## Pre-release pass (final wave, before the v0.1.0 tag)

A full run of both arms after the final fix wave, at commit f977a4f, with skill sha256 `9036c78aee7b`. The same flags and blind grading were used. The CLI was Claude Code 2.1.311. Then one targeted rerun of `dedup-key` with skill sha256 `a29663d28f40`, after the wording fix. Calls: 16 base, 20 skill samples (3 cases needed more than one), 31 grader runs, then 1 rerun and 1 grader run. That is 69 in all, against a cap of 96.

| Case | base-pass5 | skill-pass6 | skill-pass7 (rerun) |
|---|---|---|---|
| `level-from-levels` | FAIL | PASS | — |
| `level-picks-by-use-when` | FAIL | PASS | — |
| `always-ask` | FAIL | PASS | — |
| `dedup-key` | FAIL | FAIL | PASS |
| `resolve-when-fixed` | FAIL | PASS | — |
| `exit-75-follow-next` | PASS | PASS | — |
| `flush-still-queued` | FAIL | PASS | — |
| `flush-dead` | PASS | PASS | — |
| `flush-delivered` | PASS | PASS | — |
| `flush-exit-0-nothing-sent` | FAIL | PASS | — |
| `exit-69-stop` | FAIL | PASS | — |
| `exit-78-74-stop` | FAIL | PASS | — |
| `exit-64-fix-call` | PASS | PASS | — |
| `no-secrets-in-args` | FAIL | PASS | — |
| `page-shim-migrated` | PASS | PASS | — |
| `tool-not-decider` | PASS | INVALID (by design) | — |
| **Total** | 6/16 | 14/15 scored | 15/15 scored |

- **`dedup-key` (skill-pass6, FAIL).** The reply sent once with a stable key and then sent nothing on later passes, so reminders and the retry of a stranded incident never fire. SKILL.md now says to run the same `notify` with the same key on every pass. The rerun passed. Both replies are kept.
- **`tool-not-decider` is INVALID by design and not scored.** The right answer there is not to use yo-human, so the agent rightly never loads the skill. The runner marks a skill-arm reply with no skill load as INVALID, never as a FAIL.
- The final skill-arm score is 15 of 15 scored cases, combining pass 6 with the `dedup-key` rerun. One sample per case, so read it as "no miss seen", not as proof.
