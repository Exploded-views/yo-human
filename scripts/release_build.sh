#!/bin/sh
# release_build.sh OUTDIR [LDFLAGS]: build ./cmd/yo-human for every RELEASE_TARGETS target as the
# release does, into OUTDIR/<os>_<arch>/yo-human: CGO_ENABLED=0, -trimpath, LDFLAGS as -ldflags,
# no build tags, and GOFLAGS, GOEXPERIMENT, GOWORK, GOAMD64, GOARM64 and the go env file
# (GOENV=off) cleared, so the caller's shell cannot change what is built. make nohooks checks
# these binaries. Exit: 0 built, 2 a build failed or RELEASE_TARGETS is bad, 64 usage.
set -u
[ "$#" -ge 1 ] && [ "$#" -le 2 ] || { echo "usage: scripts/release_build.sh OUTDIR [LDFLAGS]" >&2; exit 64; }
out=$1
ldflags=${2-}
root=$(cd "$(dirname "$0")/.." && pwd)
targets=$(sh "$root/scripts/release-targets.sh") || exit 2
while read -r os arch; do
  mkdir -p "$out/${os}_$arch" || exit 2
  if ! (cd "$root" && env GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 GOFLAGS= GOENV=off GOEXPERIMENT= \
      GOWORK=off GOAMD64= GOARM64= go build -trimpath -ldflags "$ldflags" \
      -o "$out/${os}_$arch/yo-human" ./cmd/yo-human); then
    echo "release_build: $os/$arch: go build failed" >&2; exit 2
  fi
  echo "release_build: $out/${os}_$arch/yo-human"
done <<TARGETS
$targets
TARGETS
