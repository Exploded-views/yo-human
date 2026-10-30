# Set up Bark

Bark delivers pushes to the Bark app on iOS through Apple's push service. In yo-human it is a channel of `type: bark`. It supports all four intensities, including `critical`. It cannot update or close a notification and it cannot schedule one, so a `quiet_hours: hold` level that sends to Bark needs the [cron flush](cron-flush.md).

Every value on this page is a placeholder. Hosts are `example.com` names.

## Inputs

Export each one in your shell before you start. The device key is a secret: it goes in through 1Password, never into a variable.

- `INSTANCE="<iphone>"` — the name you give this channel under `channels:` in your config
- `OP_VAULT="<vault>"` — the 1Password vault that holds the device key
- `OP_ITEM="<item>"` — the 1Password item whose `device_key` field holds the key

## Already set

- `yo-human` is installed, and `yo-human config check` runs.
- The Bark app is installed on your iPhone and has registered. The app shows your device key and, if you self-host, your server's address.
- You saved the device key in the 1Password item.
- Your shell has `op` signed in.

## Add the channel

Add this under `channels:` in your config:

```yaml
channels:
  iphone:
    type: bark
    device_key: { secret: bark-key }
    # server: https://bark.example.com   # only for a self-hosted Bark server
```

- Without `server`, yo-human uses `https://api.day.app`, the public Bark server.
- `server` must be an https URL. yo-human never follows a redirect, so a server that starts redirecting fails pages permanently until you correct `server` in the config.
- The device key is a secret: anyone who has it can push to your phone. The config holds only its name, `{ secret: bark-key }`. A literal key fails `config check`.

Store the key. The value goes from 1Password into the secrets file and nowhere else:

```sh
op read "op://$OP_VAULT/$OP_ITEM/device_key" | yo-human secret set bark-key
```

Run this in your own terminal: it asks you to type `yes` before it stores anything.

`yo-human secret list` shows names and byte counts, never values.

## Self-hosted server with basic auth

A self-hosted bark-server can run with basic auth. It then answers every push without credentials with HTTP 418 "I'm a teapot". Give the instance a `username` and a `password`, both secrets, and yo-human sends them as HTTP basic auth on each push:

```yaml
channels:
  iphone:
    type: bark
    server: https://bark.example.com
    device_key: { secret: bark-key }
    username: { secret: bark-user }
    password: { secret: bark-password }
```

Store both, in your own terminal:

```sh
op read "op://$OP_VAULT/$OP_ITEM/username" | yo-human secret set bark-user
op read "op://$OP_VAULT/$OP_ITEM/password" | yo-human secret set bark-password
```

- Set both or neither. One without the other is refused with exit 78, and the message names the instance and both fields, never a value.
- A 418 or 401 answer fails the page permanently, with a hint to set `username` and `password`. Fix the credentials and send again.
- The public server needs none of this.

## Critical alerts need a permission

The `critical` level reaches your phone through iOS critical alerts. These need the **critical-alerts permission for the Bark app**. Grant it in the Bark app's notification settings on the iPhone. Without it, a `critical` page still arrives, but as a normal notification: it respects the silent switch and Focus modes. If a critical page does not break through, check this permission first.

## Groups: the group is the project

yo-human sets the Bark group to the message's project (`--project`, or `YO_HUMAN_PROJECT`). Each project gets its own thread in the app. You can mute a group in the Bark app. Muting that group stops its non-critical pages from lighting the screen. Use this to quiet a chatty project without touching the config.

## Per-send options

A send can set `options` for Bark. The valid ones are `sound` (a string), `call` (true or false) and `volume` (a whole number from 0 to 10):

```yaml
levels:
  - name: page
    use_when: "Production is impaired. Wakes the human."
    send:
      - channel: iphone
        interruption: critical
        options: { volume: 10, call: true }
```

Any other option is refused by `config check` with exit 78.

## Check it

```sh
yo-human config check
yo-human channel test "$INSTANCE"
```

`config check` lists any secret you have not stored yet, each with the command that stores it. A working channel prints:

```
RESULT delivered <id>
iphone: sent
```

The test exits 0 and the phone shows a passive "yo-human test message". An unreachable server prints `RESULT failed <id>` with `transient; try again`, and exits 69. A missing secret or a bad `server` exits 78 with the fix.
