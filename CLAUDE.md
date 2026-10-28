# yo-human

You must follow the constitution in [constitution.md](constitution.md) when doing any work in this repository. The constitution defines non-negotiable principles for this project; if any instruction here conflicts with it, the constitution wins.

The one way AI agents reach their human: a Go CLI, called on demand (no daemon), delivering through channels (PagerDuty, ntfy, Bark; Heeler later).

Status: M0 scaffold (Go module, `yo-human version`, CI gates). No notify yet: that is M1. Hosted mode (H1, `docs/plans/2026-10-27-hosted-h1.md`) has its skeleton: `yo-human-hosted version`, its own layer rules and the token leak rule.

## Commands

- `make build`: `dist/yo-human` and `dist/yo-human-hosted`, `CGO_ENABLED=0 -trimpath`
- `make test`: `go test -race ./...`
- `make lint`: `gofmt` check, `go vet ./...`, and `scripts/lint.sh` (golangci-lint per `RELEASE_TARGETS` target with cgo off and on; depguard enforces the layers, the CLI's and hosted mode's: `internal/hosted/` never imports the CLI side or the reverse, and only `internal/hosted/egress` makes outbound HTTP calls)
- `make lint-rules-test`: each depguard and forbidigo rule against its probes in a temp module, through `scripts/lint.sh`
- `make release-net-check`: per release target, `go list -deps ./cmd/yo-human` with the release settings imports no network package outside the channel implementations
- `RELEASE_TARGETS`: the release GOOS/GOARCH list every release-side gate reads
- `make floors`: coverage per package against `coverage-floors.json` (floors only rise)
- `make perf`: p95 of each scenario against `perf-budgets.json`
- `make secrets` / `make secrets-test`: gitleaks over full history / its tests
- `make vuln`: govulncheck (advisory per PR, blocking at release)
- `make workflows`: lint `.github/workflows`
- `make tools`: install the pinned tools; `make hooks`: install the lefthook pre-commit hook
- `make smoke`: build, then run `dist/yo-human version` and `dist/yo-human-hosted version`
- `make ci`: everything CI runs, in CI's order, without a base
- `make ci-base`: `make ci` with floors and budgets compared to the merge base with `BASE` (default `origin/main`), plus advisory `make vuln`; gaps in CONTRIBUTING.md

## Read first

- Design: `docs/specs/2026-10-26-yo-human-design.md`
- Owner decisions and open questions: `docs/OPEN-QUESTIONS.md`
- Plan: `docs/plans/2026-10-26-plan.md`
- How the owner works, plus the herdr and bitc contracts: `docs/KNOWLEDGE.md`
