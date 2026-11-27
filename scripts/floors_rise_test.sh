#!/bin/sh
# floors_rise_test.sh: scripts/floors_rise.sh fails when coverage-floors.json has a floor lower
# than, or removed from, the copy at the base commit, or an exemption the base lacks. Each case
# runs in a scratch git repo whose first commit holds the base floors; the head floors are an
# untracked file named by FLOORS_FILE, and the base is that commit (FLOORS_BASE). coverfloors is
# built once from this repo and handed to the script through COVERFLOORS.
# Exit: 0 all cases pass, 1 a case failed.
set -u
root=$(cd "$(dirname "$0")/.." && pwd)
t=$(mktemp -d) || exit 1
trap 'rm -rf "$t"' EXIT
fails=0

if [ ! -f "$root/scripts/floors_rise.sh" ]; then
  echo "FAIL scripts/floors_rise.sh is missing"
  echo "floors_rise_test: 1 case(s) failed"
  exit 1
fi
(cd "$root" && go build -o "$t/coverfloors" ./tools/coverfloors) || { echo "FAIL cannot build coverfloors"; exit 1; }

# A scratch repo must not read the developer's git config (hooks, signing).
GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.com GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.com
export GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL GIT_COMMITTER_NAME GIT_COMMITTER_EMAIL

# repo NAME BASE_JSON: $t/NAME, a repo with one commit holding BASE_JSON as coverage-floors.json;
# an empty BASE_JSON commits a README instead, so the base has no floors file.
repo() {
  mkdir -p "$t/$1" || exit 1
  git -C "$t/$1" init -q || exit 1
  if [ -n "$2" ]; then printf '%s\n' "$2" >"$t/$1/coverage-floors.json"; else echo x >"$t/$1/README"; fi
  git -C "$t/$1" add -A && git -C "$t/$1" commit -q -m base || exit 1
}

# want NAME CODE PATTERN BASEREF: runs the script in repo $t/NAME with the head floors in
# $t/NAME/head.json; fails the case unless it exits CODE and prints a line containing PATTERN.
want() {
  name=$1 code=$2 pat=$3 ref=$4
  (cd "$t/$name" && env FLOORS_BASE="$ref" FLOORS_FILE="$t/$name/head.json" COVERFLOORS="$t/coverfloors" \
    sh "$root/scripts/floors_rise.sh") >"$t/out" 2>&1
  got=$?
  if [ "$got" != "$code" ]; then
    echo "FAIL $name: exit $got, want $code"; sed 's/^/    /' "$t/out"; fails=$((fails + 1))
  elif ! grep -qF -e "$pat" "$t/out"; then
    echo "FAIL $name: no line containing '$pat'"; sed 's/^/    /' "$t/out"; fails=$((fails + 1))
  else
    echo "ok   $name"
  fi
}

repo lowered '{"example.com/a":{"statements":80.0}}'
echo '{"example.com/a":{"statements":79.9}}' >"$t/lowered/head.json"
want lowered 1 "example.com/a: floor lowered 80.0" HEAD

repo removed '{"example.com/a":{"statements":80.0},"example.com/b":{"statements":50.0}}'
echo '{"example.com/a":{"statements":80.0}}' >"$t/removed/head.json"
want removed 1 "example.com/b: floor removed" HEAD

repo exempt '{"example.com/a":{"statements":80.0}}'
echo '{"exempt":{"example.com/a":"slow"},"example.com/a":{"statements":80.0}}' >"$t/exempt/head.json"
want exempt 1 "example.com/a: exemption added" HEAD

repo raised '{"example.com/a":{"statements":80.0},"example.com/b":{"statements":50.0}}'
echo '{"example.com/a":{"statements":80.0},"example.com/b":{"statements":50.1}}' >"$t/raised/head.json"
want "equal and raised floors" 0 "floors-rise: base " HEAD

repo noref '{"example.com/a":{"statements":80.0}}'
echo '{"example.com/a":{"statements":80.0}}' >"$t/noref/head.json"
want "the ref does not resolve" 2 "cannot resolve no-such-ref" no-such-ref

repo nofile ''
echo '{"example.com/a":{"statements":1.0}}' >"$t/nofile/head.json"
want "the base has no floors file" 0 "skipped, no coverage-floors.json at base" HEAD

[ "$fails" = 0 ] && { echo "floors_rise_test: all cases pass"; exit 0; }
echo "floors_rise_test: $fails case(s) failed"
exit 1
