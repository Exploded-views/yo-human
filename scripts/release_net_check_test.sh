#!/bin/sh
# release_net_check_test.sh: tests for scripts/release_net_check.sh on scratch modules built under
# a temp dir. Each module has cmd/yo-human importing internal/core and internal/channel/registry,
# registry importing the ntfy implementation (which imports net/http), plus one variation.
# Exit: 0 all cases pass, 1 a case failed, 2 setup error.
set -u
script=$(cd "$(dirname "$0")" && pwd)/release_net_check.sh
targets=$(cd "$(dirname "$0")/.." && pwd)/RELEASE_TARGETS
t=$(mktemp -d) || exit 2
trap 'rm -rf "$t"' EXIT
trap 'exit 2' INT TERM
M=github.com/Exploded-views/yo-human
fails=0

# put FILE CONTENT: write CONTENT (printf format) to FILE in the current module.
put() { mkdir -p "$(dirname "$1")" && printf "$2" >"$1" || exit 2; }

# newmod NAME: a scratch module whose release build is clean; leaves cwd inside it.
newmod() {
  mkdir "$t/$1" && cd "$t/$1" || exit 2
  put go.mod "module $M\n\ngo 1.27\n"
  put cmd/yo-human/main.go "package main\n\nimport (\n\t_ \"$M/internal/channel/registry\"\n\t_ \"$M/internal/core\"\n)\n\nfunc main() {}\n"
  put internal/core/core.go "package core\n\nimport _ \"net/url\"\n"
  put internal/channel/registry/registry.go "package registry\n\nimport _ \"$M/internal/channel/ntfy\"\n"
  put internal/channel/ntfy/ntfy.go "package ntfy\n\nimport (\n\t_ \"$M/internal/channel/transport\"\n\t_ \"net/http\"\n)\n"
  put internal/channel/transport/transport.go "package transport\n\nimport _ \"crypto/tls\"\n"
  put internal/store/store.go "package store\n\nimport _ \"net/http\"\n"
  cp "$targets" RELEASE_TARGETS || exit 2
}

# check NAME WANT_EXIT [TEXT]: run the script in cwd; compare the exit code; TEXT must appear.
# The check runs with the caller's environment plus $checkenv (VAR=value words), so a case can
# set what a developer's shell might (GOFLAGS, CGO_ENABLED).
checkenv=
check() {
  env $checkenv sh "$script" >"$t/out" 2>&1
  got=$?
  if [ "$got" != "$2" ]; then
    echo "FAIL $1: want exit $2, got $got"; sed "s#$t#\$TMP#g; s/^/     /" "$t/out"; fails=$((fails + 1)); return
  fi
  if [ -n "${3-}" ] && ! grep -qF -- "$3" "$t/out"; then
    echo "FAIL $1: output lacks '$3'"; sed "s#$t#\$TMP#g; s/^/     /" "$t/out"; fails=$((fails + 1)); return
  fi
  echo "ok   $1 (exit $got)"
}

newmod clean
check "implementations and transport may import the network; store is not in the release build" 0

newmod negated
put internal/core/x_e2e.go "//go:build !purego\n\npackage core\n\nimport _ \"net/http\"\n"
check "core imports net/http under a negated tag" 1 "$M/internal/core imports net/http"

newmod tagged
put internal/core/x_e2e.go "//go:build e2e\n\npackage core\n\nimport _ \"net/http\"\n"
check "core imports net/http only under the e2e tag" 0

newmod registry
put internal/channel/registry/net.go "package registry\n\nimport _ \"net\"\n"
check "registry imports net" 1 "$M/internal/channel/registry imports net"

newmod rpc
put internal/core/rpc.go "package core\n\nimport _ \"net/rpc\"\n"
check "core imports net/rpc" 1 "$M/internal/core imports net/rpc"

newmod tls
put internal/core/tls.go "package core\n\nimport _ \"crypto/tls\"\n"
check "core imports crypto/tls" 1 "$M/internal/core imports crypto/tls"

newmod syslog
put internal/core/syslog.go "package core\n\nimport _ \"log/syslog\"\n"
check "core imports log/syslog" 1 "$M/internal/core imports log/syslog"

newmod allowed
put internal/core/ok.go "package core\n\nimport (\n\t_ \"net/mail\"\n\t_ \"net/netip\"\n)\n"
check "net/url, net/mail and net/netip are allowed" 0

newmod thirdparty
put go.mod "module $M\n\ngo 1.27\n\nrequire example.com/evil v0.0.0\n\nreplace example.com/evil => ./thirdparty/evil\n"
put thirdparty/evil/go.mod "module example.com/evil\n\ngo 1.27\n"
put thirdparty/evil/evil.go "package evil\n\nimport _ \"net/http\"\n"
put internal/core/evil.go "package core\n\nimport _ \"example.com/evil\"\n"
check "a third-party module in the release build imports net/http" 1 "example.com/evil imports net/http"

# The release is built for every target in RELEASE_TARGETS with CGO_ENABLED=0; the host's own
# GOOS, GOARCH and cgo setting must not decide what the check sees.
newmod nocgo
put internal/core/x.go "//go:build !cgo\n\npackage core\n\nimport _ \"net/http\"\n"
check "core imports net/http in a !cgo file (the release has cgo off)" 1 "$M/internal/core imports net/http"
checkenv=CGO_ENABLED=1
check "the same, with CGO_ENABLED=1 in the caller's environment" 1 "$M/internal/core imports net/http"
checkenv=

newmod darwin
put internal/core/x_darwin.go "package core\n\nimport _ \"net/http\"\n"
check "core imports net/http in an _darwin.go file" 1 "darwin/arm64: $M/internal/core imports net/http"

newmod linux
put internal/core/x_linux.go "package core\n\nimport _ \"net/http\"\n"
check "core imports net/http in an _linux.go file" 1 "linux/amd64: $M/internal/core imports net/http"

newmod arm64
put internal/core/x_arm64.go "package core\n\nimport _ \"net/http\"\n"
check "core imports net/http in an _arm64.go file" 1 "linux/arm64: $M/internal/core imports net/http"

# Control: a file built only with cgo on never ships (every release target has CGO_ENABLED=0), so
# it is outside the release graph and this check passes it. The lint gates still deny it: they
# also lint each target with cgo on, because tests build with cgo (go test -race).
newmod cgotag
put internal/core/x.go "//go:build cgo\n\npackage core\n\nimport _ \"net/http\"\n"
check "a cgo-only file is outside the release graph" 0

# A developer's GOFLAGS (here the e2e tag) must not change the graph the check reads.
newmod goflags
put internal/core/x_e2e.go "//go:build e2e\n\npackage core\n\nimport _ \"net/http\"\n"
checkenv=GOFLAGS=-tags=e2e
check "GOFLAGS=-tags=e2e in the caller's environment is ignored" 0
checkenv=
# The same flag from a go env file (go env -w GOFLAGS=...): an empty GOFLAGS does not override
# it; only GOENV=off does.
printf 'GOFLAGS=-tags=e2e\n' >"$t/goenv"
checkenv=GOENV=$t/goenv
check "GOFLAGS=-tags=e2e in a go env file is ignored" 0
checkenv=

# A go.work changes what the go tool builds; the check refuses to guess.
newmod gowork
put go.work "go 1.27\n\nuse .\n"
check "a go.work in the module" 2 "go.work"

newmod notargets
put RELEASE_TARGETS "# none\n"
check "RELEASE_TARGETS lists no target" 2 "RELEASE_TARGETS"

newmod badtarget
put RELEASE_TARGETS "linux/amd64\nplan10/z80\n"
check "RELEASE_TARGETS names a target go does not know" 2 "plan10/z80"

newmod broken
put internal/core/broken.go "package core\n\nimport _ \"$M/internal/missing\"\n"
check "go list fails" 2

[ "$fails" = 0 ] || { echo "release_net_check_test: $fails failure(s)"; exit 1; }
echo "release_net_check_test: all cases pass"
