# Example: a release is ready to publish

A release can wait for the human, so it goes out as a `note`: low urgency, never critical. The release URL is also an action, so the human can open it from the message.

## Inputs

Export each one in your shell before you start.

- `VERSION="<1.4.0>"` — the version that is ready to publish
- `RELEASE_URL="<https://example.com/releases/1.4.0>"` — the draft release the human opens to publish it

## Already set

- `yo-human` is installed, and `yo-human config check` runs.
- Your config has a `note` level (the starter config does). Run `yo-human levels --json` to see the human's own names.
- The release is built and its draft is saved.

## Send it

```sh
yo-human notify --level note \
  --title "Release $VERSION is ready to publish" \
  --ask "Publish it when you next have a minute" \
  --action "Open release=$RELEASE_URL" \
  --dedup-key "release-$VERSION"
```

The dedup key is `release-` and the version, so running this twice for one release does not tell the human twice.
