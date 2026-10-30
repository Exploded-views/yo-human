#!/bin/sh
# release_build_test.sh: scripts/release_build.sh builds every RELEASE_TARGETS target as the
# release does (CGO_ENABLED=0, -trimpath, the given -ldflags, no tags, the default GOAMD64 and
# GOARM64), whatever the caller's shell sets. It runs the script in a hostile environment and
# reads each binary's build settings back with go version -m.
# Exit: 0 all checks pass, 1 a check failed, 2 setup error.
set -u
root=$(cd "$(dirname "$0")/.." && pwd)
t=$(mktemp -d) || exit 2
trap 'rm -rf "$t"' EXIT
trap 'exit 2' INT TERM
fails=0
fail() { echo "FAIL $1"; fails=$((fails + 1)); }

printf 'GOFLAGS=-tags=e2e\n' >"$t/goenv" || exit 2
(cd "$root" && env CGO_ENABLED=1 GOFLAGS=-tags=e2e GOENV="$t/goenv" GOEXPERIMENT=boringcrypto \
  GOWORK="$t/go.work" GOAMD64=v3 GOARM64=v9.0 GOOS=windows GOARCH=386 \
  sh scripts/release_build.sh "$t/out" "-s -w -X github.com/Exploded-views/yo-human/internal/version.Version=9.9.9-releasebuildprobe") \
  >"$t/log" 2>&1 || { fail "release_build.sh exited $?"; sed 's/^/     /' "$t/log"; }

targets=$(sh "$root/scripts/release-targets.sh") || exit 2
n=0
while read -r os arch; do
  n=$((n + 1))
  bin="$t/out/${os}_$arch/yo-human"
  if ! go version -m "$bin" >"$t/m" 2>&1; then fail "$os/$arch: no readable binary at ${os}_$arch/yo-human"; continue; fi
  for want in "GOOS=$os" "GOARCH=$arch" "CGO_ENABLED=0" "-trimpath=true"; do
    grep -qE "^[[:space:]]+build[[:space:]]+$want\$" "$t/m" || fail "$os/$arch: build setting $want missing"
  done
  case $arch in
    amd64) grep -qE '^[[:space:]]+build[[:space:]]+GOAMD64=v1$' "$t/m" || fail "$os/$arch: GOAMD64 is not the default v1" ;;
    arm64) grep -qE '^[[:space:]]+build[[:space:]]+GOARM64=v8.0$' "$t/m" || fail "$os/$arch: GOARM64 is not the default v8.0" ;;
  esac
  grep -qE '^[[:space:]]+build[[:space:]]+-tags=' "$t/m" && fail "$os/$arch: built with build tags"
  grep -qE '^[[:space:]]+build[[:space:]]+GOEXPERIMENT=' "$t/m" && fail "$os/$arch: built with a GOEXPERIMENT"
  # go version -m leaves -ldflags out under -trimpath, so the flags are checked by their effect:
  # the -X version string is in the binary, and -s left an ELF binary no symbol table (Mach-O
  # keeps a dynamic one whatever -s says, so darwin is not checked).
  grep -qa 9.9.9-releasebuildprobe "$bin"
  case $? in 0) ;; 1) fail "$os/$arch: the -X version is not in the binary" ;; *) fail "$os/$arch: grep failed" ;; esac
  [ "$os" = linux ] && go tool nm "$bin" >/dev/null 2>&1 && fail "$os/$arch: the binary has a symbol table (no -s)"
done <<TARGETS
$targets
TARGETS
extra=$(find "$t/out" -name yo-human -type f 2>/dev/null | wc -l | tr -d ' ')
[ "$extra" = "$n" ] || fail "built $extra binaries for $n targets"

[ "$fails" = 0 ] || { echo "release_build_test: $fails failure(s)"; exit 1; }
echo "release_build_test: all $n targets built as the release"
