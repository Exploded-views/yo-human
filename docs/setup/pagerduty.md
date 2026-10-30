# Set up PagerDuty

PagerDuty pages you through its mobile app, phone calls and SMS. In yo-human it is a channel of `type: pagerduty`, which uses the Events API v2. It updates an open alert when the same dedup key repeats, and it closes the alert on `yo-human resolve`. It cannot schedule a page, so a `quiet_hours: hold` level that sends to PagerDuty needs the [cron flush](cron-flush.md).

Every value on this page is a placeholder.

## Inputs

Export each one in your shell before you start. A routing key is a secret: it goes in through 1Password, never into a variable.

- `CRITICAL_INSTANCE="<pd-critical>"` — the channel name for the service that wakes you
- `LOW_INSTANCE="<pd-low>"` — the channel name for the service that does not
- `OP_VAULT="<vault>"` — the 1Password vault that holds the routing keys
- `OP_ITEM_CRITICAL="<item>"` — the item whose `routing_key` field holds the critical service's key
- `OP_ITEM_LOW="<item>"` — the item whose `routing_key` field holds the low service's key

## Already set

- `yo-human` is installed, and `yo-human config check` runs.
- You have a PagerDuty account (the Free plan is enough) with the mobile app set up for notifications.
- Two services exist, each with an **Events API v2** integration. One service is configured for high urgency and one for low. Each integration shows a routing key (also called an integration key).
- You saved each routing key in its 1Password item.
- Your shell has `op` signed in.

## One service per urgency

On the Free plan, urgency is set per **service**, not per alert. A loud page and a quiet page are therefore two services, each with its own routing key, and so two yo-human channel instances. This is also why a PagerDuty send takes no per-send options: the routing key belongs to the instance, and a different key is a different instance. Setting `options` on a PagerDuty send fails `config check` with exit 78.

```yaml
channels:
  pd-critical:
    type: pagerduty
    routing_key: { secret: pd-critical }
  pd-low:
    type: pagerduty
    routing_key: { secret: pd-low }
```

Then send to the instance whose urgency you want at each level:

```yaml
levels:
  - name: heads-up
    use_when: "The human should look soon, but nothing is on fire."
    send:
      - { channel: pd-low, interruption: active }
  - name: page
    use_when: "Production is impaired. Wakes the human."
    send:
      - { channel: pd-critical, interruption: critical }
```

yo-human maps the interruption to the alert's severity: `passive` is `info`, `active` is `warning`, `time-sensitive` is `error` and `critical` is `critical`. The service, not the severity, decides whether PagerDuty phones you.

## Store the routing keys

The routing keys are secrets. The config holds only their names. A literal key fails `config check`. The values go from 1Password into the secrets file and nowhere else:

```sh
op read "op://$OP_VAULT/$OP_ITEM_CRITICAL/routing_key" | yo-human secret set pd-critical
op read "op://$OP_VAULT/$OP_ITEM_LOW/routing_key" | yo-human secret set pd-low
```

Run this in your own terminal: it asks you to type `yes` before it stores anything.

`yo-human secret list` shows names and byte counts, never values.

## Dedup keys and resolving

- The dedup key you pass with `--dedup-key` is at most **255 characters**; a longer one is refused with exit 64. PagerDuty's `dedup_key` is that key with the project in front, `<length of the project>:<project>/<key>` (for example `4:proj/disk-7`), so two projects that use the same key have two alerts. A pair longer than 255 bytes is sent as `h:` and its sha256. Without a key, yo-human uses the message id, so the alert can still be resolved by that id.
- A repeat with the same key updates the open alert and does not page again. `yo-human resolve --dedup-key <key>` closes it. PagerDuty's resolve event carries no note, so the `--note` text is not shown there.
- **PagerDuty folds repeats into the open alert.** A level's `reminder`, and a louder repeat sent to the same PagerDuty instance, go out with the same `dedup_key`, so PagerDuty adds them to the alert that is already open instead of paging you again. yo-human still reports them `sent`, because PagerDuty accepted them. If a louder repeat must page again, send the louder level to another instance (another service, whose urgency is high).
- If PagerDuty cannot be reached when you resolve, the alert stays open and keeps escalating until the close gets through. `resolve` then exits 75 with a `next` step (`yo-human flush`), which retries the close. A close PagerDuty refuses for good exits 69: resolve the alert in the PagerDuty app.
- `events_url` is optional and overridable (for example for a test server). It defaults to `https://events.pagerduty.com/v2/enqueue` and **must be https**. yo-human never follows a redirect.

## Check it

```sh
yo-human config check
yo-human channel test "$CRITICAL_INSTANCE"
yo-human channel test "$LOW_INSTANCE"
```

Each test sends a passive (severity `info`) alert through that one service, so it can open a real alert on that service. Resolve it from the PagerDuty app afterwards. `config check` lists any secret you have not stored yet, each with the command that stores it. A working channel prints:

```
RESULT delivered <id>
pd-critical: sent
```

The test exits 0. A rejected key prints `RESULT failed <id>` and exits 69. A missing secret or a bad `events_url` exits 78 with the fix.
