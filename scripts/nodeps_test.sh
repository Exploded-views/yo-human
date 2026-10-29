#!/bin/sh
# nodeps_test.sh: scripts/nodeps.sh fails when a forbidden package is among the dependencies, and
# a failing go list or grep, or a listing too short to prove anything, fails it too, never passes.
# Exit: 0 all cases pass, 1 a case failed.
set -u
root=$(cd "$(dirname "$0")/.." && pwd)
t=$(mktemp -d) || exit 1
trap 'rm -rf "$t"' EXIT
fails=0

# want NAME CODE CMD...: runs CMD, and fails the case unless it exits CODE.
want() {
  name=$1 code=$2; shift 2
  "$@" >"$t/out" 2>&1
  got=$?
  if [ "$got" != "$code" ]; then
    echo "FAIL $name: exit $got, want $code"; sed 's/^/    /' "$t/out"; fails=$((fails + 1))
  else
    echo "ok   $name"
  fi
}

# stub NAME BODY: a fake command on $t/NAME/ for PATH.
stub() {
  mkdir -p "$t/$1"
  printf '#!/bin/sh\n%s\n' "$3" >"$t/$1/$2"
  chmod +x "$t/$1/$2"
}
stub clean go 'printf "runtime\nos\ngopkg.in/yaml.v3\n"'
stub dirty go 'printf "runtime\nos\ngithub.com/santhosh-tekuri/jsonschema/v6\n"'
stub broken go 'echo "go: cannot load" >&2; exit 1'
stub empty go 'exit 0'
stub badgrep grep 'echo "grep: failed" >&2; exit 2'
stub partial go 'printf "runtime\nos\n"; echo "go: a package failed to load" >&2; exit 1'
stub latergrep grep 'case "$1" in -qx) exec /usr/bin/grep "$@";; esac; echo "grep: failed" >&2; exit 2'

want "a clean listing" 0 env PATH="$t/clean:$PATH" sh "$root/scripts/nodeps.sh" jsonschema -deps ./cmd/yo-human
want "a forbidden package" 1 env PATH="$t/dirty:$PATH" sh "$root/scripts/nodeps.sh" jsonschema -deps ./cmd/yo-human
want "go list fails" 2 env PATH="$t/broken:$PATH" sh "$root/scripts/nodeps.sh" jsonschema -deps ./cmd/yo-human
want "an empty listing proves nothing" 2 env PATH="$t/empty:$PATH" sh "$root/scripts/nodeps.sh" jsonschema -deps ./cmd/yo-human
want "go list fails after printing" 2 env PATH="$t/partial:$PATH" sh "$root/scripts/nodeps.sh" jsonschema -deps ./cmd/yo-human
want "grep fails" 2 env PATH="$t/badgrep:$t/clean:$PATH" sh "$root/scripts/nodeps.sh" jsonschema -deps ./cmd/yo-human
want "grep fails on the search" 2 env PATH="$t/latergrep:$t/clean:$PATH" sh "$root/scripts/nodeps.sh" jsonschema -deps ./cmd/yo-human
want "no pattern" 64 sh "$root/scripts/nodeps.sh"
# --release: the listing is taken once per RELEASE_TARGETS target with the release env
# (CGO_ENABLED=0; GOFLAGS, GOENV, GOEXPERIMENT, GOWORK, GOAMD64 and GOARM64 cleared), whatever
# the caller's shell sets. The stub lists the forbidden package only for the target $HIT and only
# under exactly that env, so a target skipped or a setting not cleared passes it.
realgo=$(command -v go) || { echo "nodeps_test: no go on PATH"; exit 1; }
stub release go "case \"\$1\" in tool) exec \"$realgo\" \"\$@\" ;; esac
printf 'runtime\\nos\\n'
if [ \"\$GOOS/\$GOARCH\" = \"\$HIT\" ] && [ \"\$CGO_ENABLED\" = 0 ] && [ -z \"\$GOFLAGS\" ] && [ \"\$GOENV\" = off ] &&
  [ -z \"\$GOEXPERIMENT\" ] && [ \"\$GOWORK\" = off ] && [ -z \"\$GOAMD64\" ] && [ -z \"\$GOARM64\" ]; then
  echo github.com/santhosh-tekuri/jsonschema/v6
fi"
hostile="CGO_ENABLED=1 GOFLAGS=-tags=e2e GOENV=$t/goenv GOEXPERIMENT=boringcrypto GOWORK=$t/go.work GOAMD64=v3 GOARM64=v9.0"
for hit in $(sh "$root/scripts/release-targets.sh" | tr ' ' /); do
  want "--release: a package only $hit's release build holds" 1 env PATH="$t/release:$PATH" HIT="$hit" $hostile sh "$root/scripts/nodeps.sh" --release jsonschema -deps ./cmd/yo-human
  grep -qF "$hit" "$t/out" || { echo "FAIL --release: the report does not name $hit"; fails=$((fails + 1)); }
done
want "--release: no target's release build holds it" 0 env PATH="$t/release:$PATH" HIT=none $hostile sh "$root/scripts/nodeps.sh" --release jsonschema -deps ./cmd/yo-human
want "--release: no pattern" 64 sh "$root/scripts/nodeps.sh" --release

# The real module: the binary is clean, and the config tests, which hold the reference, are not.
want "the binary has no jsonschema" 0 sh "$root/scripts/nodeps.sh" jsonschema -deps ./cmd/yo-human
want "no release target's binary has jsonschema" 0 sh "$root/scripts/nodeps.sh" --release jsonschema -deps ./cmd/yo-human
want "the config tests do import jsonschema" 1 sh "$root/scripts/nodeps.sh" jsonschema -deps -test ./internal/config

[ "$fails" = 0 ] && { echo "nodeps_test: all cases pass"; exit 0; }
echo "nodeps_test: $fails case(s) failed"; exit 1
