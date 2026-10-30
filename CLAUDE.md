# yo-human

You must follow the constitution in [constitution.md](constitution.md) when doing any work in this repository. The constitution defines non-negotiable principles for this project; if any instruction here conflicts with it, the constitution wins.

The one way AI agents reach their human: a Go CLI, called on demand (no daemon), delivering through channels (PagerDuty, ntfy, Bark; Heeler later).

Status: the M0 scaffold and M1 (v1 notify) exist: `notify`, `resolve`, `flush`, `status`, `levels`, `page`, `channel test`, `config init|check|schema|trust` and `secret set|list`, over ntfy, Bark and PagerDuty. The contract is `docs/specs/2026-10-26-m1-contract-design.md` with its amendments; the agent skill is `skills/yo-human/SKILL.md`. Hosted mode (H1, `docs/plans/2026-10-27-hosted-h1.md`) has its skeleton: `yo-human-hosted version`, its own layer rules and the token leak rule.

## Commands

- `make build`: `dist/yo-human` and `dist/yo-human-hosted`, `CGO_ENABLED=0 -trimpath`
- `make test`: `go test -race ./...`
- `make e2e`: the real binary (built with `-tags e2e`) against fake ntfy, Bark and PagerDuty servers
- `make nohooks`: builds every `RELEASE_TARGETS` target as the release does and checks that none holds either e2e test hook; `make nodeps`: no target's binary links the JSON Schema library
- `make lint`: `gofmt` check, `go vet ./...`, and `scripts/lint.sh` (golangci-lint per `RELEASE_TARGETS` target with cgo off and on; depguard enforces the layers, the CLI's and hosted mode's: `internal/hosted/` never imports the CLI side or the reverse, and only `internal/hosted/egress` makes outbound HTTP calls), then `go vet` and `golangci-lint` with `-tags e2e`
- `make lint-rules-test`: each depguard and forbidigo rule against its probes in a temp module, through `scripts/lint.sh`
- `make release-net-check`: per release target, `go list -deps ./cmd/yo-human` with the release settings imports no network package outside the channel implementations
- `RELEASE_TARGETS`: the release GOOS/GOARCH list every release-side gate reads
- `make floors`: coverage per package against `coverage-floors.json` (floors only rise)
- `make perf`: p95 of each scenario against `perf-budgets.json`
- `make secrets` / `make secrets-test`: gitleaks over full history / its tests
- `make vuln`: govulncheck (advisory per PR, blocking at release)
- `make release-check`: a goreleaser snapshot, each archive checked against `checksums.txt` and for test hooks (needs the network and a clone with its git history; not in `make ci`)
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
