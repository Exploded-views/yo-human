MODULE  := github.com/Exploded-views/yo-human
VERSION := $(shell git describe --tags --always --dirty | sed 's/^v//')
COMMIT  := $(shell git rev-parse --short=12 HEAD)
LDFLAGS := -s -w -X $(MODULE)/internal/version.Version=$(VERSION) -X $(MODULE)/internal/version.Commit=$(COMMIT)

.PHONY: build smoke test lint lint-rules-test release-net-check tools hooks hooks-check hooks-test workflows ci ci-base cover floors perf secrets secrets-test vuln

# The release's settings: CGO_ENABLED=0; no GOFLAGS, GOEXPERIMENT or go env file from the
# developer's setup; no go.work.
# Both binaries: the CLI and hosted mode.
build:
	CGO_ENABLED=0 GOFLAGS= GOENV=off GOEXPERIMENT= GOWORK=off go build -trimpath -ldflags "$(LDFLAGS)" -o dist/yo-human ./cmd/yo-human
	CGO_ENABLED=0 GOFLAGS= GOENV=off GOEXPERIMENT= GOWORK=off go build -trimpath -ldflags "$(LDFLAGS)" -o dist/yo-human-hosted ./cmd/yo-human-hosted

# CI's test job runs each built binary once.
smoke: build
	./dist/yo-human version
	./dist/yo-human-hosted version

test:
	go test -race ./...

# gofmt's own status is checked as well as its output: a syntax error (say in a _windows.go file
# that vet and golangci-lint skip on this OS) fails the gate. Tracked and untracked-not-ignored
# files only, so dist/ stays out. golangci-lint runs once per RELEASE_TARGETS target with cgo off
# and once with it on (scripts/lint.sh), so no host setting hides a file.
lint:
	files=$$(git ls-files -co --exclude-standard '*.go') && test -n "$$files" || exit 1; \
	out=$$(gofmt -l $$files) || exit 1; \
	test -z "$$out" || { echo "gofmt needs to run on:"; echo "$$out"; exit 1; }
	go vet ./...
	sh scripts/lint.sh

# The release build (go list -deps ./cmd/yo-human, no tags) imports no network package outside
# the channel implementations; then the check's own tests.
release-net-check:
	sh scripts/release_net_check.sh
	sh scripts/release_net_check_test.sh

# Every depguard rule against its probes, in a temp module (scripts/lint_rules_test.sh).
lint-rules-test:
	sh scripts/lint_rules_test.sh

tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
	go install golang.org/x/vuln/cmd/govulncheck@v1.8.0
	go install github.com/zricethezav/gitleaks/v8@v8.30.1
	go install github.com/evilmartians/lefthook/v2@v2.2.2

hooks:
	lefthook install

hooks-check:
	sh scripts/check-hooks.sh

# check-hooks.sh's own tests, then the hooks end to end in a scratch repo (needs lefthook).
hooks-test:
	sh scripts/check_hooks_test.sh
	sh scripts/hooks_test.sh

workflows:
	sed 's/  # .*//' tools/check-workflows.sha256 | shasum -a 256 -c - && python3 tools/check-workflows.py

cover:
	mkdir -p .cache
	go list ./... > .cache/pkgs.txt
	go test -race -coverprofile=.cache/cover.out ./...

floors: cover
	go run ./tools/coverfloors --profile .cache/cover.out --packages .cache/pkgs.txt $(FLOORS_ARGS)

perf: build
	go run ./tools/perf $(PERF_ARGS)

secrets:
	scripts/secrets.sh

secrets-test:
	sh scripts/secrets_test.sh

vuln:
	govulncheck ./...

ci: hooks-check hooks-test workflows lint lint-rules-test release-net-check test smoke floors perf secrets secrets-test

# ci plus what CI does with a base: floors and budgets compared with the merge base of HEAD and
# BASE (default origin/main), and govulncheck, advisory as in CI (its failure is reported and
# ignored). Gaps CI has that this cannot reproduce are listed in CONTRIBUTING.md.
BASE ?= origin/main
ci-base: hooks-check hooks-test workflows lint lint-rules-test release-net-check test smoke secrets secrets-test
	BASE=$(BASE) sh scripts/ci-base.sh
	-$(MAKE) vuln
