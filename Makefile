# Pinned like the tools; the same version is in .github/workflows/release.yml. No global install.
GORELEASER ?= go run github.com/goreleaser/goreleaser/v2@v2.18.3

MODULE  := github.com/Exploded-views/yo-human
VERSION := $(shell git describe --tags --always --dirty | sed 's/^v//')
COMMIT  := $(shell git rev-parse --short=12 HEAD)
LDFLAGS := -s -w -X $(MODULE)/internal/version.Version=$(VERSION) -X $(MODULE)/internal/version.Commit=$(COMMIT)

.PHONY: build smoke test e2e nohooks nodeps lint lint-rules-test release-net-check tools hooks hooks-check hooks-test workflows ci ci-base cover floors perf secrets secrets-test scripts-test vuln release-check

# The release's settings: CGO_ENABLED=0; no GOFLAGS, GOEXPERIMENT, GOAMD64, GOARM64 or go env
# file from the developer's setup; no go.work.
# Both binaries: the CLI and hosted mode.
build:
	CGO_ENABLED=0 GOFLAGS= GOENV=off GOEXPERIMENT= GOWORK=off GOAMD64= GOARM64= go build -trimpath -ldflags "$(LDFLAGS)" -o dist/yo-human ./cmd/yo-human
	CGO_ENABLED=0 GOFLAGS= GOENV=off GOEXPERIMENT= GOWORK=off GOAMD64= GOARM64= go build -trimpath -ldflags "$(LDFLAGS)" -o dist/yo-human-hosted ./cmd/yo-human-hosted

# CI's test job runs each built binary once.
smoke: build
	./dist/yo-human version
	./dist/yo-human-hosted version

test:
	go test -race ./...

# End to end: the real binary, built with -tags e2e (two test-only hooks, the clock and a root
# certificate for the TLS fakes), against fake ntfy, Bark and PagerDuty servers. The release build
# has neither hook: nohooks builds every RELEASE_TARGETS target as the release does
# (scripts/release_build.sh) and checks that none holds either variable name, and release-check
# checks the same of every release archive.
e2e:
	go test -tags e2e ./test/e2e/ -count=1

nohooks:
	rm -rf dist/release
	sh scripts/release_build.sh dist/release "$(LDFLAGS)"
	scripts/nohooks.sh dist/release/*/yo-human

# No release target's binary carries a JSON Schema library: config validation is hand-written, and
# the library is only the tests' reference (its package init cost every command about 2ms).
nodeps:
	scripts/nodeps.sh --release jsonschema -deps ./cmd/yo-human

# gofmt's own status is checked as well as its output: a syntax error (say in a _windows.go file
# that vet and golangci-lint skip on this OS) fails the gate. Tracked and untracked-not-ignored
# files only, so dist/ stays out; tracked files deleted in the work tree are skipped. golangci-lint
# runs once per RELEASE_TARGETS target with cgo off and once with it on (scripts/lint.sh), so no
# host setting hides a file. The e2e files are vetted and linted on the host with -tags e2e.
lint:
	deleted=$$(git ls-files -d); \
	files=$$(git ls-files -co --exclude-standard '*.go' | sort -u | grep -vxF -e "$$deleted"); \
	test -n "$$files" || exit 1; \
	out=$$(gofmt -l $$files) || exit 1; \
	test -z "$$out" || { echo "gofmt needs to run on:"; echo "$$out"; exit 1; }
	go vet ./...
	sh scripts/lint.sh
	go vet -tags e2e ./...
	golangci-lint run --build-tags e2e

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

# The scripts that count matches tell no match from a grep failure, and the release build loop
# builds every target as the release does.
scripts-test:
	sh scripts/grep_status_test.sh
	sh scripts/nodeps_test.sh
	sh scripts/release_build_test.sh

vuln:
	govulncheck ./...

# Not in ci (it needs the network to fetch goreleaser); release.yml runs it before publishing. It
# needs a clone with its git history, and refuses a shallow one or a directory outside git. A
# snapshot build, then every archive is verified against checksums.txt: a missing archive, a
# missing entry or a mismatch fails.
release-check:
	@test "$$(git rev-parse --is-shallow-repository 2>/dev/null)" = false || { \
	  echo "release-check: needs a clone with its full git history (goreleaser reads the tags); in CI, checkout with fetch-depth: 0"; exit 1; }
	$(GORELEASER) check
	$(GORELEASER) release --snapshot --clean
	cd dist && n=0 && while read -r sum name; do \
	  test -f "$$name" || { echo "release-check: $$name is listed in checksums.txt but missing"; exit 1; }; \
	  n=$$((n + 1)); \
	done < checksums.txt && test "$$n" -eq 4 || { echo "release-check: checksums.txt must list 4 archives"; exit 1; }
	cd dist && for a in yo-human_*.tar.gz; do \
	  grep -q "  $$a$$" checksums.txt || { echo "release-check: $$a is not in checksums.txt"; exit 1; }; \
	done
	cd dist && shasum -a 256 -c checksums.txt
	scripts/nohooks.sh dist/yo-human_*.tar.gz
	@echo "release-check: 4 archives match checksums.txt"

ci: hooks-check hooks-test workflows lint lint-rules-test release-net-check test e2e smoke nohooks nodeps floors perf secrets secrets-test scripts-test

# ci plus what CI does with a base: floors and budgets compared with the merge base of HEAD and
# BASE (default origin/main), and govulncheck, advisory as in CI (its failure is reported and
# ignored). Gaps CI has that this cannot reproduce are listed in CONTRIBUTING.md.
BASE ?= origin/main
ci-base: hooks-check hooks-test workflows lint lint-rules-test release-net-check test e2e smoke nohooks nodeps secrets secrets-test scripts-test
	BASE=$(BASE) sh scripts/ci-base.sh
	-$(MAKE) vuln
