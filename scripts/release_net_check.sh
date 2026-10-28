#!/bin/sh
# release_net_check.sh: the release-side backstop for Constitution IV's single network path, run
# in the module in the current directory. For every target in its RELEASE_TARGETS file (or
# $RELEASE_TARGETS) it lists the release build's packages exactly as the release builds them:
# CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go list -deps ./cmd/yo-human, with no build tags, and
# GOFLAGS, GOEXPERIMENT and the go env file (GOENV=off: an empty GOFLAGS does not override a
# `go env -w GOFLAGS`) cleared, and GOWORK=off (a go.work in the module is refused instead). It fails when, for any
# target, a non-standard package outside the channel implementations
# (<module>/internal/channel/<name>, registry excluded), a third-party module included, imports
# net, a net/ package other than net/url, net/mail and net/netip, crypto/tls or log/syslog.
# It proves this for those targets with cgo off, and nothing about other targets, cgo-on builds
# (test binaries) or files the go tool does not build for them; the lint gates cover those.
# Standard-library packages are not checked. Exit: 0 clean, 1 violations (one line each, naming
# the target), 2 go list failed, a go.work is present, or RELEASE_TARGETS is missing, empty or
# names an unknown target.
set -u
d=$(mktemp -d) || exit 2
trap 'rm -rf "$d"' EXIT
trap 'exit 2' INT TERM
here=$(cd "$(dirname "$0")" && pwd)
if [ -e go.work ]; then
  echo "release_net_check: go.work in the module changes what the go tool builds; remove it"; exit 2
fi
targets=$(sh "$here/release-targets.sh" "${RELEASE_TARGETS:-RELEASE_TARGETS}") || exit 2
if ! mod=$(GOFLAGS= GOENV=off GOEXPERIMENT= GOWORK=off go list -m 2>"$d/err"); then
  echo "release_net_check: go list -m failed:"; cat "$d/err"; exit 2
fi
bad=0
while read -r os arch; do
  if ! CGO_ENABLED=0 GOOS=$os GOARCH=$arch GOFLAGS= GOENV=off GOEXPERIMENT= GOWORK=off go list -deps \
      -f '{{if not .Standard}}{{.ImportPath}}{{range .Imports}} {{.}}{{end}}{{end}}' \
      ./cmd/yo-human >"$d/deps" 2>"$d/err"; then
    echo "release_net_check: $os/$arch: go list -deps ./cmd/yo-human failed:"; cat "$d/err"; exit 2
  fi
  while read -r pkg imports; do
    [ -z "$pkg" ] && continue
    case "$pkg" in
      "$mod"/internal/channel/registry | "$mod"/internal/channel/registry/*) ;;
      "$mod"/internal/channel/*) continue ;; # an implementation (or transport): may use the network
    esac
    for imp in $imports; do
      case "$imp" in
        net/url | net/mail | net/netip) ;;
        net | net/* | crypto/tls | log/syslog) echo "release_net_check: $os/$arch: $pkg imports $imp"; bad=1 ;;
      esac
    done
  done <"$d/deps"
done <<TARGETS
$targets
TARGETS
[ "$bad" = 0 ] || exit 1
echo "release_net_check: no release target ($(echo "$targets" | tr ' ' / | tr '\n' ' ' | sed 's/ $//')) imports a network package outside the channel implementations"
