# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `docs/paging-guide.md` maps common events (an outage, a decision, a green PR, a vendor incident,
  a release, a finished task) onto the starter levels, with three copyable examples in
  `docs/examples/`. A repolint check holds them to the starter config's level names.
- A page on a paging level (one with a critical send) that is delivered more than 5 minutes after it
  was queued, such as one retried after an outage, has its title prefixed `[queued since HH:MM UTC]`,
  the time it was queued, so a late page does not pass for a fresh one. Resolves, resolve notes and
  levels with no critical send are not marked. A page held for quiet hours counts from the end of the
  hold.

## [v0.2.0] - 2026-11-02

### Added

- `yo-human page --check` prints the shim's level, ask and sends for `page` and `page --low`, and the
  agent, without sending anything, so a host can see the mapping before it migrates.
- `yo-human page version` and `page help` (any case, with or without `--low`) are usage errors (64)
  and send nothing, as `page --version` and `-v` already were; `page -- version` sends that word as
  the summary on purpose.
- `yo-human page` refuses a page flag typed after the summary (`page "test" --check`,
  `page x --low`) with a usage error (64), instead of paging with the flag as the details;
  `page -- x --check` still sends that text on purpose.

### Changed

- `yo-human page --resolve <key>` no longer answers `noop` for a key yo-human holds no open incident
  for: it resolves the alert on the instances a record lists, on a single other project's incident
  (only when no `YO_HUMAN_PROJECT` is set), or on every resolving instance of the shim level (with
  the project's namespaced key when a project is set, the bare key when not), so a key past
  `incident_ttl` is closed in PagerDuty. An alert the old script opened with a bare key is closed
  only when the resolve runs with `YO_HUMAN_PROJECT` unset; with a project set, only the project's
  own namespaced key is resolved, so resolve a pre-cutover alert from a shell with it unset. A key
  already closed is re-sent only to channels that can resolve, with no new ntfy note. A key written
  in yo-human's internal provider-key form is refused (64).
- A send whose request went out but whose answer never came (a timeout, or on Bark a 2xx reply cut
  off) is no longer retried on ntfy or Bark below a paging level (a level with a critical send), so
  a push the server already delivered is not delivered twice. The channel is reported `sent`, with
  `unconfirmed` in `--json` and `log.jsonl` and a note in the text. Paging levels, PagerDuty, and
  failures before the request went out retry as before.
- Bark: an error answer whose body breaks off is classified by its status, as a whole error answer
  is. A 5xx, 408 or 429 is still retried; any other 4xx, which was retried before, is now refused.

## [v0.1.1] - 2026-10-31

### Added

- Bark: optional `username` and `password` secrets on a bark channel. yo-human sends them as
  HTTP basic auth on every push, for a self-hosted bark-server that requires it. Set both or
  neither; one alone is refused with exit 78. A 418 or 401 answer now says the server wants basic
  auth or rejected it.

### Fixed

- `yo-human config init` checks that the starter log path can be written into the config before
  it asks the human to approve a `--config` write and before it creates any directory, and it
  refuses with exit 74.

## [v0.1.0] - 2026-10-30

M1, the first usable release: an agent can reach its human from the command line. This also ships
the M0 scaffold.

### Added

- `yo-human notify`: sends a message to the human through the configured channels, with a
  deadline, deduplication by key, up to three actions and http(s) links, quiet hours, and a
  retry queue. The first stdout line is always `RESULT <status> <id>`; `--json` prints the same
  as a versioned object. Exit codes: 0 done, 64 usage, 69 failed, 70 internal, 74 store I/O,
  75 queued, 78 config. A level sends through every channel that can be built; one that cannot
  (a missing secret) is reported `skipped`, with exit 78 once nothing else is pending. A louder
  repeat, or a reminder, replaces the copy still held for quiet hours, so the human is paged once.
- `yo-human resolve`: closes an incident. It first cancels everything still pending for it
  (queued and held sends, and messages ntfy holds for later), closes the alert on channels that
  can (PagerDuty), and sends a passive "resolved" note to the others the incident reached. A
  cancel or a PagerDuty close that is queued for another try exits 75 with `next`; one refused
  for good exits 69.
- `yo-human flush`: sends what the retry queue holds that is due.
- `yo-human status`: shows incidents and the queue (`RESULT ok`).
- `yo-human page`: the drop-in shim for the old paging script, keeping its arguments
  (`[--low] "<summary>" [details] [dedup-key]` and `--resolve <dedup-key>`), mapped onto levels by
  the config's `shim:` block. `yo-human levels` and `yo-human channel test`.
- `yo-human --help`, `-h` and `help` (also after a command) print the usage and exit 0.
- Config in layers, the closest winning (system, user, project), with trusted project files;
  `yo-human config check|init|schema` and a starter config; `yo-human secret set` for secrets kept in a
  0600 file; `yo-human config trust` for project config files.
- Channels: ntfy, Bark and PagerDuty.
- Security (task 16 review): `config trust` and `secret set` are the human's, confirmed by typing
  `yes` at the controlling terminal (the human's own scripts may set an environment variable,
  audited in `log.jsonl`); a `--config` file outside `$XDG_CONFIG_HOME/yo-human/` needs the
  human's trust and may not define a log channel; incidents are namespaced by project
  (`resolve --project`, `status --project|--all`); the page shim's default key gains a random
  suffix; `--body-file` takes a regular file outside the config and secrets, and stored secret
  values are redacted from messages; project files must be the user's alone and the search stops
  at `HOME`; `HOME` (or absolute XDG directories) is required; log paths must be absolute;
  `--project`/`--agent` are at most 64 characters; the trust display shows every setting,
  escaped. Round 2: with `YO_HUMAN_PROJECT` set, another project is out of reach; providers see
  `<len>:<project>/<key>`; `config init --config` is the human's; `--body-file` stays inside the
  project. Round 3: flags go after the command. See the spec's amendments 13 to 31.
- The incident lock is waited for only until the call's deadline. A keyed notify that cannot
  check its incident in time waits in the queue for a flush (exit 75), and a resolve says to run
  again. See the spec's amendments 32 onwards.
- An agent skill with setup pages for each channel.
- Release tooling: goreleaser builds linux and darwin on amd64 and arm64 (static, `-trimpath`)
  with a sha256 `checksums.txt`, run by a tag-triggered workflow that fails on any
  `govulncheck` finding and checks the archives; `make release-check` verifies a snapshot build.
- M0 scaffold: Go module and `yo-human version` CLI skeleton, `make` targets
  (`build smoke test lint lint-rules-test tools hooks workflows cover floors perf secrets
  secrets-test vuln ci ci-base`; M1 adds `e2e nohooks nodeps scripts-test release-check`),
  GitHub Actions CI on hosted runners, workflow lint,
  repo size and complexity lint rules, per-package coverage floors,
  performance budgets, gitleaks secret scan, and `docs/tdd/` RED records.
- Repo docs and templates: MIT license, security policy, contributing guide,
  CODEOWNERS, pull request template and task and decision issue forms.

### Changed

- README rewritten for new readers: why yo-human exists, the planned M1 CLI
  (marked as not shipped), a message-path diagram, build-from-source steps and
  the milestone roadmap. Theme-aware banner in `docs/assets/`.

[Unreleased]: https://github.com/Exploded-views/yo-human/compare/v0.2.0...HEAD
[v0.2.0]: https://github.com/Exploded-views/yo-human/compare/v0.1.1...v0.2.0
[v0.1.1]: https://github.com/Exploded-views/yo-human/compare/v0.1.0...v0.1.1
[v0.1.0]: https://github.com/Exploded-views/yo-human/releases/tag/v0.1.0
