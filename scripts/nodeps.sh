#!/bin/sh
# Usage: scripts/nodeps.sh [--release] <regex> <go list arguments>...
# Fails (exit 1) when a package that `go list <arguments>` prints matches <regex>: make nodeps keeps
# a library out of the binary's dependencies. With --release the listing is taken once per
# RELEASE_TARGETS target with the release's env (CGO_ENABLED=0; GOFLAGS, GOEXPERIMENT, GOWORK,
# GOAMD64, GOARM64 and the go env file cleared), so neither the host nor the caller's shell
# decides what is listed. A go list or grep that fails exits 2, never passing as no match; so does
# a listing without the runtime package, which would prove nothing.
set -u
release=0
if [ "${1-}" = --release ]; then release=1; shift; fi
[ "$#" -ge 2 ] || { echo "usage: scripts/nodeps.sh [--release] <regex> <go list arguments>..." >&2; exit 64; }
cd "$(dirname "$0")/.." || exit 2
pattern=$1
shift
tmp=$(mktemp) || exit 2
trap 'rm -f "$tmp" "$tmp.hits"' EXIT

# check LABEL GO-LIST-ARGS...: list, then search; exits on a hit (1) or a failure (2).
check() {
  label=$1
  shift
  go list "$@" >"$tmp" || { echo "nodeps: ${label}go list $* failed" >&2; exit 2; }
  # grep -x runtime is the control; a match of the pattern is the failure.
  grep -qx runtime "$tmp"
  case $? in
  0) ;;
  1) echo "nodeps: ${label}go list $* does not list runtime, so the search proves nothing" >&2; exit 2 ;;
  *) echo "nodeps: grep failed" >&2; exit 2 ;;
  esac
  grep -E -- "$pattern" "$tmp" >"$tmp.hits"
  case $? in
  0) echo "nodeps: ${label}go list $* holds packages matching $pattern:" >&2; sed 's/^/  /' "$tmp.hits" >&2; exit 1 ;;
  1) echo "nodeps: ${label}go list $* holds no package matching $pattern" ;;
  *) echo "nodeps: grep failed" >&2; exit 2 ;;
  esac
}

if [ "$release" = 0 ]; then
  check "" "$@"
  exit 0
fi
targets=$(sh scripts/release-targets.sh) || exit 2
while read -r os arch; do
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 GOFLAGS= GOENV=off GOEXPERIMENT= GOWORK=off GOAMD64= GOARM64=
  export GOOS GOARCH CGO_ENABLED GOFLAGS GOENV GOEXPERIMENT GOWORK GOAMD64 GOARM64
  check "$os/$arch: " "$@"
done <<TARGETS
$targets
TARGETS
