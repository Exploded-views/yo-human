---
name: yo-human
description: Use when you need to reach your human through the yo-human CLI (notify, resolve, flush, levels, page), or when a yo-human call exits non-zero. It covers the call recipe, choosing a level, dedup keys and what to do for each exit code. It does not decide whether to notify; your own rules do.
---

# yo-human

`yo-human` is a tool, not a decision-maker. Your own rules and operating model decide
**whether** the human is needed and **how loud**. This skill only covers how to make the call
once they have. If your rules say nothing is needed, do not call it because it is installed.

It is a plain CLI: no daemon, a bounded call (about 1 second), and it never loses a message.
Run commands as `yo-human <command>`; `--json` works on every command and prints one object.
Flags always go after the command (`yo-human status --json`); one before it exits 64.
`yo-human help` (or `--help`, `-h`, also after a command) prints the usage and exits 0.

## Pick a level from the human's own ladder

Never guess level names. They are the human's, and they differ per person.

```
yo-human levels --json
```

Prints `{"version":1,"status":"ok","levels":[{"name","use_when","ask","quiet_hours"}, ...]}`,
quietest first. Choose the level whose `use_when` matches your situation. Read it once per
session. A wrong name exits 64 and the error lists the valid ones.

## Send

```
yo-human notify --level <name> --title "<what happened>" --ask "<what the human must do>" \
  [--body "<detail>" | --body-file <path> | --body -] [--link <https url>] \
  [--action "<label>=<https url>"]... [--dedup-key <key>]
```

- Always pass `--ask`, one or two sentences saying what the human must do. The `ask` field of
  `levels` says `required` for most levels; pass it even when it is `optional`.
- `--title` is at most 200 characters; `--ask` at most 1000; `--body` at most 16 KB; at most 3
  `--action`; links are http(s) only. Put detail in `--body`, not in the title.
- **Dedup key.** Pass `--dedup-key` for anything that may repeat or that you will later fix: a
  watcher in a loop, a failing job, a health check. Use a stable key that names the problem
  (`disk-full-build-host`), never a timestamp or counter. A repeat at the same or a lower level
  is suppressed (exit 0: an earlier page for this incident is pending or was delivered; run
  `yo-human status` if unsure), so the human is not paged twice; a louder repeat goes through, and so
  does one after the level's `reminder` interval (if it has one), or after every send of the
  incident failed for good and none reached the human. In a loop, run the same `notify` with the
  same key on every pass while the problem lasts. Don't stop after the first send: those repeats
  are what deliver the reminder and retry an incident that never reached the human.
- **Never put a secret in any argument or in the body**: tokens, keys, passwords, connection
  strings. Name where it is (the file or log) and tell the human to rotate it. Arguments show up in process
  lists and shell history.
- Set `YO_HUMAN_PROJECT` and `YO_HUMAN_AGENT` so the message says who sent it. Incidents belong to
  the project: `resolve` and `status` see only your project's (`status --all` lists every project).

## Resolve when it is fixed

```
yo-human resolve --dedup-key <same key> [--note "<short note>"]
```

This cancels anything still queued for that key and tells the channels the human was reached
on that it is over. Run it once the problem is fixed and verified. Do not send a new notify to
say "fixed". An unknown or already-resolved key exits 0 with `RESULT noop`, and so does a key
whose incident expired (silent past the config's `incident_ttl`).

## Trust and secrets are the human's

Never run `yo-human config trust` or `yo-human secret set`, and never try to get around the prompt
they show: they belong to the human, in their own terminal. Never pass `--config` or set `YO_HUMAN_CONFIG` yourself;
use `--config` only where a printed `next.do` carries it. Do not pass `--project` for another
project, and do not run `config init`. `--body-file` takes a file inside your working directory or
project. When an error or notice says a file is not trusted or a secret is not set, stop and tell
the human, quoting it.

## Output and exit codes

The first stdout line is `RESULT <status> <id>` (`RESULT ok` for `levels`, `status`, `config`).
Errors go to stderr as `error:`, `valid:` and `fix:` lines (in `--json`, an `error` object).

| Exit | Status | What you do |
|---|---|---|
| 0 | delivered, partial, held, suppressed, resolved, noop | Done. Carry on. `held` means it waits for the human's quiet hours to end; that is intended. |
| 64 | (usage) | Your call is wrong. Read `error`/`valid`/`fix`, fix the call, run it again. |
| 75 | queued | No channel reachable yet. Follow `next` (below), then tell the human. `flush` exits 75 too, while a send is still queued; so does `resolve` while a cancel or a PagerDuty close is queued. |
| 69 | failed | Every channel rejected it for good. **Stop.** Retrying cannot help, so do not retry or switch levels. Tell the human in your session. |
| 78 | (config) | Missing secret, bad config or loose permissions. **Stop.** Tell the human; only they can fix it. Quote the `error` and `fix` lines. Do not edit the config or secrets yourself. A channel listed `skipped` could not be built (say, its secret is missing): the level's other channels still went out, and the `error` names the one to fix. A skipped channel is not queued or retried: it stays unreached until the human fixes it and you send again. |
| 74 | (store I/O) | **Stop.** Tell the human in your session. |
| 70 | (internal bug) | **Stop.** Tell the human in your session. |

`flush` has the same codes, for the sends it tried in that run. Exit 0 means nothing failed and nothing is
left to retry *in this run*: read the channel lines, where `sent` means delivered and no lines at all
means nothing was due yet (not delivery). Exit 75 with `RESULT queued` and `next` means a send is still
queued; 69 with `RESULT failed` means one was dead-lettered.
`resolve` exits 75 with `next` when cancelling a scheduled send, or closing the PagerDuty alert,
failed for now and is queued: follow `next`, since until it runs the human may still be paged for
something fixed. It exits 69 when one was refused for good: tell the human (the page may still
fire, or the alert is still open in PagerDuty).

### Exit 75: follow `next`, bounded

```
next: yo-human flush in 60s, 5 attempts left, then tell the human in your session
```

In `--json` that is `next: {"do","after_seconds","attempts_left","then"}`. Wait `after_seconds`, then run
`next.do` exactly as printed (it may carry `--config <path>`; do not retype it). It is usually
`yo-human flush`; when another yo-human process held the incident lock past the deadline, a
`resolve` gets its own command again as `next.do`, which is safe to repeat. Read the result: exit 0
with `sent` means delivered, stop; exit 75 means still queued: wait the new `next`'s `after_seconds` and run it again. Run `flush` at most as many
times as the `attempts_left` of the first `next` you were given (the one `notify` printed); each new `next`
counts down from there. Stop at the first exit 0 with `sent` (delivered) or exit 69 (failed for good). If the
last allowed flush still exits 75, the attempts have run out: tell the human directly in your session, with the
title and the ask, and do not run `flush` again for this message, not later and not "if it seems useful". Do not
re-run `notify` (that makes a duplicate) and do not change the key. Never retry without a bound. Never take a flush exit 0 as
delivery without reading its channel lines.

### Telling the human in your session

For 69, 74, 78, 70 and an exhausted 75, the human has not been reached. Say so plainly in your reply
or your handoff: what you tried to tell them, the ask, and the error text. Do not hide the failure
and do not route around it with another tool.

## Migrated callers: `page`

`yo-human page` exists only for hosts whose old paging script was replaced by
`exec yo-human page "$@"`. It keeps the old arguments and takes no `--level`, `--title` or `--ask`;
the config's `shim:` block maps them to levels.

```
yo-human page [--low] "<summary>" [details] [dedup-key]
yo-human page [--low] --resolve <dedup-key>
```

Default is the "need the human now" service; `--low` is for "CI/CD broken while production is up".
Put `--low` before the summary. Exit codes are the same as above. A trigger prints the key it used
on a `key:` line (`dedup_key` in `--json`), so a page sent without a key can still be resolved.
New code uses `notify`, not `page`.
Always pass a dedup key as the last argument (use `""` for no details): without one every call is
its own incident, named by the clock, and you could not resolve it. `--resolve` needs the same key.
