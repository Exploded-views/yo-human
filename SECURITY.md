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

## Supported versions

Pre-release: only the latest commit on `main` is supported.
