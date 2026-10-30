# Set up ntfy

ntfy delivers pushes to the ntfy app on iOS and Android, from a server you run. In yo-human it is a channel of `type: ntfy`. It can deliver at a later time itself, so a `quiet_hours: hold` level needs no cron entry for it.

Every value on this page is a placeholder. Hosts are `example.com` names, and the only secrets are the ones you store with `yo-human secret set`.

## Inputs

Export each one in your shell before you start. Do not paste a real topic into any of them: the topic is a secret and goes in through 1Password.

- `INSTANCE="<phone>"` — the name you give this channel under `channels:` in your config
- `NTFY_SERVER="<https://ntfy.example.com>"` — your ntfy server; it must be https
- `OP_VAULT="<vault>"` — the 1Password vault that holds the topic
- `OP_ITEM="<item>"` — the 1Password item whose `topic` field holds the topic

## Already set

- `yo-human` is installed, and `yo-human config check` runs (see the config starter from `yo-human config init`).
- An ntfy server is running behind https, at version **v2.16.0 or later**.
- The topic is a long random string of 1 to 64 letters, digits, `-` or `_`, saved in the 1Password item. You subscribed the ntfy app to it.
- Your shell has `op` signed in.

## Server version

The ntfy server must be **v2.16.0 or later**. yo-human cancels a held page when you `resolve` its incident, and it does that with `sequence_id` and an HTTP `DELETE`. On an older server the cancel fails: the held page still fires after the resolve, and yo-human reports the failed cancel: `resolve` exits 75 (with `next`) if the cancel can be retried, or 69 if it cannot. Check the version before you rely on holds.

```sh
curl -fsS "$NTFY_SERVER/v1/health"
```

A healthy server answers with `{"healthy":true}`. That only proves it is up. The version is the one you installed on the server host.

## Instant delivery to iOS

A self-hosted server reaches an iPhone instantly only if it forwards a poll request through ntfy.sh. Set this on the **server** (`server.yml`), not in yo-human:

```yaml
base-url: https://ntfy.example.com
upstream-base-url: https://ntfy.sh
```

Only the topic and the message id transit ntfy.sh. Without `upstream-base-url` iOS shows the push late, or only when the app is opened.

## Add the channel

Add this under `channels:` in your config, with your own instance name and server:

```yaml
channels:
  phone:
    type: ntfy
    server: https://ntfy.example.com
    topic: { secret: ntfy-topic }
    # token: { secret: ntfy-token }   # only if the server requires a login
```

- `server` must be an https URL. A plain `http://` server is refused with exit 78.
- The topic is a secret, because anyone who knows it can read and publish to it. It is only ever a secret name (`{ secret: ntfy-topic }`) in the config. A literal topic fails `config check`.
- yo-human never follows a redirect from the server, so a server that starts redirecting fails pages until you correct `server`.

Store the topic. The value goes from 1Password into the secrets file and nowhere else:

```sh
op read "op://$OP_VAULT/$OP_ITEM/topic" | yo-human secret set ntfy-topic
```

Run this in your own terminal: it asks you to type `yes` before it stores anything.

If the server needs a token, store it the same way as `ntfy-token`. `yo-human secret list` shows names and byte counts, never values.

## What to know when you choose levels

- **Priorities:** `passive`, `active` and `time-sensitive` send ntfy priorities 2, 3 and 4.
- **No critical:** ntfy has no iOS critical alerts, so yo-human sends no priority 5. A level that sends `critical` to an ntfy channel makes `yo-human config check` print a warning. Send critical pages through Bark or PagerDuty, and keep ntfy at `time-sensitive` or below.
- **Links:** set `options: { markdown: true }` on a send to have the app render markdown. That includes links the agent supplies, so enable it only for agents you trust. It is off by default.
- **Two instances, one topic:** the message id doubles as ntfy's `sequence_id`. Two instances that share a topic share message ids, so one can replace or cancel the other's message. Give each instance its own topic.

## Check it

```sh
yo-human config check
yo-human channel test "$INSTANCE"
```

`config check` lists any secret you have not stored yet, each with the command that stores it. A working channel prints:

```
RESULT delivered <id>
phone: sent
```

The test exits 0 and the phone shows a passive "yo-human test message". A server that cannot be reached prints `RESULT failed <id>` with `transient; try again`, and exits 69. A missing secret or a bad `server` exits 78 with the fix.
