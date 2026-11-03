# Example: watch a vendor's status and relay an incident

A vendor incident that affects you is a `heads-up`. The watcher polls a status URL and exits on any change. It sends one notify per incident, under one dedup key, and resolves that key when the vendor recovers. Recovery is a `resolve`, not a new notify.

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
last=$(curl -fsS "$STATUS_URL")
while sleep "$POLL_SECONDS"; do
  now=$(curl -fsS "$STATUS_URL") || continue
  [ "$now" = "$last" ] || break
done

if [ "$now" = ok ]; then
  yo-human resolve --dedup-key "vendor-$VENDOR-incident"
else
  yo-human notify --level heads-up --dedup-key "vendor-$VENDOR-incident" \
    --title "$VENDOR reports an incident" --ask "Check whether it affects us" \
    --body "Status changed from $last to $now."
fi
rc=$?
```

Read `rc` as the skill's exit-code table says:

- `0`: done. This includes `suppressed` (an earlier send for this incident is still pending or was delivered) and `held`.
- `75`: queued. Leave it to `yo-human flush`, and follow the `next` line it printed, bounded by its `attempts_left`. Do not run `notify` again.
- `64`: your call is wrong. Read the `error` and `fix` lines and correct the call.
- `69`, `78`, `74`, `70`: stop and tell the human in your session, quoting the `error` and `fix` lines.
