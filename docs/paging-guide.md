# Paging guide: which event gets which level

Level names are the human's own and come from their config. Run `yo-human levels --json` and choose by each level's `use_when`. The names below (`note`, `heads-up`, `page`) are the starter config's, from `yo-human config init`, and are used here only as examples.

Paging means "production is down or impaired". Everything else belongs on a quieter level, so a page stays rare enough to be believed.

| Event | Example level | Why |
|---|---|---|
| Production is down or impaired | `page` | It wakes the human. This is the only row that is `page`. |
| Something is blocked on the human's decision | `heads-up` | Pass `--ask` with the options and the default, so one reply answers it: `--ask "Ship A or B? Default A if no reply by noon"`. |
| A CI-only change is green and ready to merge | `heads-up` | Add the PR link as `--action "Open PR=<url>"`. See [ci-ready-link](examples/ci-ready-link.md). |
| A vendor incident affects you | `heads-up` | Use one `--dedup-key` per incident, and resolve it on recovery. See [vendor-incident](examples/vendor-incident.md). |
| The vendor recovered | `yo-human resolve --dedup-key <same key>` | This closes the incident. It is not a new notify. |
| A release is ready to publish | `note` | Low urgency, never critical: a release can wait for the human. See [release-ready](examples/release-ready.md). |
| A long task finished, nothing needed | `note` | The human reads it when they next look. |

## Rules of thumb

- A critical send is only for production down or impaired. If a level sends `critical`, only a real outage belongs on it.
- Use one dedup key per incident, named for the problem (`vendor-acme-incident`), never a timestamp or a counter.
- Resolve what you raised: `yo-human resolve --dedup-key <key>` once it is fixed and verified.
- Repeat the same call with the same key while the problem lasts. yo-human suppresses repeats until the level's `reminder` is due, so repeating never nags. Never change the key to get a fresh send.
- When unsure, take the quieter level and say why in `--body`.

## More

- The call recipe, the dedup rules and every exit code are in [skills/yo-human/SKILL.md](../skills/yo-human/SKILL.md).
- Copyable examples: [release-ready](examples/release-ready.md), [vendor-incident](examples/vendor-incident.md) and [ci-ready-link](examples/ci-ready-link.md).
