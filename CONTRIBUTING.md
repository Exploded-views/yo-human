# Contributing

[constitution.md](constitution.md) is the source of truth for how this repo
works. This page is the short path through it; where the two disagree, the
constitution wins. [CLAUDE.md](CLAUDE.md) covers the same ground for AI agents.

## Setup

```sh
make tools   # installs the pinned linters, scanners and lefthook
make hooks   # installs the git hooks
```

The hooks (lefthook, see `lefthook.yml`):

- **pre-commit** runs the fast checks every commit must pass: gofmt on the
  staged files, `go vet`, `golangci-lint` on new issues, a gitleaks scan of the
  staged diff (`scripts/secrets.sh --staged`) and the e2e build-tag check. It
  runs no tests, so a RED commit (a deliberately failing test) passes the hooks
  like any other commit.
- **pre-push** runs `go test -race ./...`. A RED commit is never pushed as a
  branch tip on its own: its fix goes up with it, so the tests pass at push.
- `--no-verify` is never used, for commits or pushes. If a hook blocks a
  commit that should go in, fix the hook in a PR; don't skip it.
- `make ci` fails when either hook is missing (`make hooks` installs both),
  and `make hooks-test` proves the hooks behave as above in a scratch repo.

Run `make ci-base` before every push. It runs `make ci` (every gate, in CI's
order) plus what CI does with a base: coverage floors and perf budgets compared
with the merge base of HEAD and `BASE` (default `origin/main`; pass
`BASE=<commit>` in a clone without it), and `make vuln`, advisory as in CI.
When the base commit has no floors or budgets file, that comparison is skipped,
as in CI. `make ci` alone needs no base.

What `make ci-base` cannot reproduce:

- The OS matrix. CI runs the test and perf jobs on both `ubuntu-latest` and
  `macos-latest`; a local run covers only your OS.
- Perf headroom. CI sets `CI=true`, which applies the budgets' headroom; a
  local run has none, so it is stricter, not looser.
- Push events. On a push to `main`, CI compares against the previous tip
  (`github.event.before`), not a merge base.
- The toolchain. CI pins Go 1.27.3 with `GOTOOLCHAIN=local` and installs the
  pinned linters fresh; locally you get whatever `make tools` installed.

## Work from an issue

Open an issue with the **task** or **decision** form (blank issues are off).
Reference it (`#123`) in the commit body and the PR.

yo-human was built in a private repository before it went public as a single
commit. Issue and PR numbers in documents from that time (for example "#57" or
"owner #8") refer to that history, which isn't public. They don't point at
issues in this repository. The same goes for short commit SHAs and links to
GitHub Actions runs in those documents: they name commits and runs in the
private history.

## Test first

Write the failing test, run it, and record the failure before writing the code
that makes it pass. Records live in [docs/tdd/](docs/tdd/):

- `docs/tdd/<topic>-red.txt` holds the raw output of the failing run.
- `docs/tdd/<topic>-mutants.txt` holds the mutation check on the key line.
- Capture raw command output with its exit status, never edited by hand:
  `cmd > docs/tdd/<topic>-red.txt 2>&1; echo "exit=$?" >> docs/tdd/<topic>-red.txt`
- Add a row for each file to the table in [docs/tdd/README.md](docs/tdd/README.md).
- Fake values only in records and fixtures (`example.com`, `R0UTING-KEY-FAKE`).

## Commits

- Conventional commits with a scope: `feat(router): ...`, `fix(pagerduty): ...`;
  `test:`, `docs:`, `refactor:`, `chore:`, `perf:` and `ci:` may omit it.
- The RED commit is the one allowed failing-test commit and says `(RED)` in its
  subject: `test(router): route by severity (RED)`. It goes through the
  pre-commit hook like every other commit, never `--no-verify`.
- Atomic commits; every other commit builds and passes.

## Branches and PRs

- `main` is protected. Use a topic branch: `feat/...`, `fix/...`, `chore/...`,
  `docs/...` or `ci/...`.
- Stacked work uses stacked branches and PRs, each based on its parent. Say in
  the PR description which PR it stacks on.
- The PR template carries the checklist from the constitution; tick it
  honestly, and justify any principle violation in writing.
- Contract changes (CLI behavior) need a CHANGELOG entry, a version bump and a
  skill update.

## Hosted mode

`yo-human-hosted` (`internal/hosted/...`) follows the same constitution, with
its own layer chain and contract (Principles IV–VIII, "Hosted mode"). It
shares no code with the CLI. Its contract is the MCP server's tool list,
schemas and error codes, versioned by `"v"`. Agents, the chief of staff
included, never resolve an approval; only the owner's passkey assertion does
(Principle VIII). Auth, tokens, approvals and infrastructure changes get an
independent security review on the exact commit.

## Security

Report vulnerabilities privately; see [SECURITY.md](SECURITY.md).

By contributing you agree your work is released under the [MIT license](LICENSE).
