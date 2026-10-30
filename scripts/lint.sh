#!/bin/sh
# lint.sh [golangci-lint args...]: golangci-lint once for each release target in RELEASE_TARGETS
# with CGO_ENABLED=0, the release's build, then once on the host with CGO_ENABLED=1, the setting
# tests build with (go test -race needs cgo). A file only some target or cgo setting builds
# (x_darwin.go, x_arm64.go, //go:build !cgo or cgo) is therefore always linted, whatever the
# host. The cgo pass is host-only: cgo for another GOOS needs that target's C toolchain (stdlib
# packages such as plugin fail to type-check without it). GOFLAGS, GOEXPERIMENT, the go env file
# (GOENV=off), GOWORK, GOAMD64 and GOARM64 (their feature tags, amd64.v3 and the like) are cleared so a developer's shell or `go env -w` cannot change the
# build. Exit: 0 clean, 1 issues in any pass, 2 golangci-lint failed or RELEASE_TARGETS is bad.
set -u
targets=$(sh "$(dirname "$0")/release-targets.sh") || exit 2
worst=0
note() { # keep the worst code: an error (2) beats issues (1) beats clean (0)
  if [ "$1" -gt 1 ]; then worst=2; elif [ "$1" = 1 ] && [ "$worst" = 0 ]; then worst=1; fi
}
while read -r os arch; do
  echo "== golangci-lint GOOS=$os GOARCH=$arch CGO_ENABLED=0"
  env GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 GOFLAGS= GOENV=off GOEXPERIMENT= GOWORK=off GOAMD64= GOARM64= golangci-lint run "$@"
  note $?
done <<TARGETS
$targets
TARGETS
echo "== golangci-lint host GOOS/GOARCH CGO_ENABLED=1"
env -u GOOS -u GOARCH CGO_ENABLED=1 GOFLAGS= GOENV=off GOEXPERIMENT= GOWORK=off GOAMD64= GOARM64= golangci-lint run "$@"
note $?
exit "$worst"
