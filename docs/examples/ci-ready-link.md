# Example: a CI-only change is green and ready to merge

The change is checked and waiting on the human's merge, which is a `heads-up`: they should look soon, nothing is on fire. The PR link is an action, and `--ask` says what to do.

## Inputs

Export each one in your shell before you start.

- `PR_NUMBER="<123>"` — the pull request number
- `PR_TITLE="<Pin the CI runner image>"` — the pull request title
- `PR_URL="<https://example.com/org/repo/pull/123>"` — the pull request link

## Already set

- `yo-human` is installed, and `yo-human config check` runs.
- Your config has a `heads-up` level (the starter config does).
- The pull request's checks are green.

## Send it

```sh
yo-human notify --level heads-up \
  --title "$PR_TITLE is ready" \
  --ask "Merge or say what to change" \
  --action "Open PR=$PR_URL" \
  --dedup-key "pr-$PR_NUMBER-ready"
```
