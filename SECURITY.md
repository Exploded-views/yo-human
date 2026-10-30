# Security policy

## Reporting a vulnerability

Report vulnerabilities privately through GitHub private vulnerability
reporting: open the **Security** tab and choose **Report a vulnerability**.
Don't open a public issue, pull request or discussion for a suspected
vulnerability, and don't paste real secrets into a report; fake values are
enough to show the problem.

Fixes land through the normal PR workflow, with a CHANGELOG entry once there is
a release; the advisory is published when the fix is out.

## In scope

The security rules are in [constitution.md](constitution.md), principle VIII.
In scope:

- **Secret handling**: any path where a secret value reaches logs, output,
  fixtures, error messages or the repository.
- **Channel isolation**: one channel implementation reaching another channel's
  credentials, state or messages.
- **Reply authenticity**: an inbound reply accepted as the human's when it is
  not, or routed to the wrong request.
- **The CI secret scan**: ways to get a secret past `make secrets` (gitleaks)
  or the scan job in CI.
- **CI and supply chain**: a way for a pull request from a fork to obtain a
  token with write access or a secret, or to change what a pinned action or
  tool runs.

## The boundary between agents and the human

yo-human is driven by agents in many harnesses (Claude Code, Codex, opencode and others) and by the
human's own scripts. The line is the same everywhere:

- **Agents may run** `notify`, `resolve`, `status` and `flush` (and the read-only `levels`, and
  `page` where a host's old paging script was replaced). These act only on the project named by
  `YO_HUMAN_PROJECT`, which the harness sets.
- **Only the human changes credentials and config:** `config trust`, `config init --config <path>`
  and `secret set`.
- **The harness's own permission rules are the primary control.** Allow the agent commands and deny
  the human ones, as [docs/setup/README.md](docs/setup/README.md#agent-harness-setup) shows.
  yo-human takes flags only after the command, so a prefix rule matches every form of a command;
  an absolute-path or wrapped invocation needs its own deny entry where rules match by prefix.
- **The terminal confirmation is defence in depth.** `config trust`, `config init --config` and
  `secret set` ask for a typed `yes` on the controlling terminal (`/dev/tty`). That holds only when
  the harness gives the agent no terminal. A harness that runs commands in a pseudo-terminal the
  agent can type into (an interactive exec mode) lets the agent answer the prompt itself, so there
  the deny rules are the only control.
- `YO_HUMAN_NONINTERACTIVE=1` skips the prompt for the human's own provisioning scripts and is
  audited in `log.jsonl`. It must never be set in an agent's environment.

## Supported versions

Pre-release: only the latest commit on `main` is supported.
