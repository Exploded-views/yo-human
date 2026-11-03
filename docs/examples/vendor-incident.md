# Example: watch a vendor's status and relay an incident

A vendor incident that affects you is a `heads-up`. The watcher polls a status URL. While the status is not `ok` it runs the same `notify`, with the same dedup key, on every pass: yo-human suppresses a repeat until the level's `reminder` is due, so repeating never nags, and it is what retries an incident that never reached the human. When the status is `ok` again it runs `resolve` and exits. Recovery is a `resolve`, not a new notify.

## Inputs

Export each one in your shell before you start.

- `VENDOR="<acme>"` — a short name for the vendor, used in the dedup key
- `STATUS_URL="<https://status.example.com/api/status.txt>"` — a URL that answers `ok` when all is well and anything else during an incident
- `POLL_SECONDS="<60>"` — seconds between polls

## Already set

- `yo-human` is installed, and `yo-human config check` runs.
- Your config has a `heads-up` level (the starter config does).
- `YO_HUMAN_PROJECT` and `YO_HUMAN_AGENT` are set, so the message says who sent it.

## Watch

```sh
: "${POLL_SECONDS:?}"
check() { curl -fsS --max-time 20 "$STATUS_URL"; }
key="vendor-$VENDOR-incident"
now=$(check) || exit 69
sent=
while :; do
  sleep "$POLL_SECONDS" || exit 64
  now=$(check) || continue
  if [ "$now" != ok ]; then
    yo-human notify --level heads-up --dedup-key "$key" \
      --title "$VENDOR reports an incident" --ask "Check whether it affects us" \
      --body "$(printf 'Status: %.200s' "$now")"
    rc=$?
    [ "$rc" -eq 0 ] || exit "$rc"
    sent=1
  elif [ -n "$sent" ]; then
    yo-human resolve --dedup-key "$key"
    exit $?
  fi
done
```

A bad `POLL_SECONDS` (empty, or still the placeholder) exits 64 before anything is sent, and so does any failed `sleep`. A first fetch that fails exits 69. A later failed poll is skipped and tried again on the next pass. Read the exit code as the skill's table says:

- `0`: done. This includes `suppressed` (an earlier send for this incident is still pending or was delivered) and `held`. The loop goes on.
- `75`: queued. The watcher stops with 75. Follow the `next` line it printed (usually `yo-human flush`), bounded by its `attempts_left`; if the last attempt still exits 75, stop and tell the human in your session. Do not run `notify` again for that message.
- `64`: your call is wrong. Read the `error` and `fix` lines and correct the call.
- `69`, `78`, `74`, `70`: stop and tell the human in your session, quoting the `error` and `fix` lines.
