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

# want NAME CODE PATTERN BASEREF [ENV]: (NAME is the case and its repo's directory) runs the script
# in repo $t/NAME with the head floors in $t/NAME/head.json; fails the case unless it exits CODE and
# prints a line containing PATTERN. BASEREF - leaves FLOORS_BASE unset; ENV is one more NAME=VALUE.
# BASE and FLOORS_BASE from the caller never leak in. $cf is the COVERFLOORS setting (empty runs
# the script's default path).
cf="COVERFLOORS=$t/coverfloors"
want() {
  name=$1 code=$2 pat=$3 ref=$4 extra=${5:-}
  fb="FLOORS_BASE=$ref"
  [ "$ref" = - ] && fb=
  (cd "$t/$name" && env -u BASE -u FLOORS_BASE FLOORS_FILE="$t/$name/head.json" $cf $fb $extra \
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
want raised 0 "floors-rise: base " HEAD

repo noref '{"example.com/a":{"statements":80.0}}'
echo '{"example.com/a":{"statements":80.0}}' >"$t/noref/head.json"
want noref 2 "cannot resolve no-such-ref" no-such-ref

repo nofile ''
echo '{"example.com/a":{"statements":1.0}}' >"$t/nofile/head.json"
want nofile 0 "skipped, no coverage-floors.json at base" HEAD

# BASE alone names the base when FLOORS_BASE is unset (make ci-base BASE=<commit>).
repo baseonly '{"example.com/a":{"statements":80.0}}'
echo '{"example.com/a":{"statements":79.9}}' >"$t/baseonly/head.json"
want baseonly 1 "example.com/a: floor lowered 80.0" - BASE=HEAD

# A local branch named origin/main must not shadow the remote-tracking ref (the default base).
repo shadow '{"example.com/a":{"statements":80.0}}'
git -C "$t/shadow" update-ref refs/remotes/origin/main HEAD
echo '{"example.com/a":{"statements":70.0}}' >"$t/shadow/coverage-floors.json"
git -C "$t/shadow" commit -q -am lower
git -C "$t/shadow" branch origin/main
echo '{"example.com/a":{"statements":75.0}}' >"$t/shadow/head.json"
want shadow 1 "example.com/a: floor lowered 80.0" -

# A base ahead of HEAD that raised a floor fails, and the output says to merge main in.
repo ahead '{"example.com/a":{"statements":80.0}}'
git -C "$t/ahead" checkout -q -b main-ahead
echo '{"example.com/a":{"statements":85.0}}' >"$t/ahead/coverage-floors.json"
git -C "$t/ahead" commit -q -am raise
git -C "$t/ahead" checkout -q -
echo '{"example.com/a":{"statements":80.0}}' >"$t/ahead/head.json"
want ahead 1 "floors-rise: main-ahead has moved past this commit; merge main in and re-run" main-ahead

# A real lowered floor against a base that is an ancestor of HEAD does not claim the base moved on.
repo nohint '{"example.com/a":{"statements":80.0}}'
echo '{"example.com/a":{"statements":79.9}}' >"$t/nohint/head.json"
want nohint 1 "example.com/a: floor lowered 80.0" HEAD
if grep -qF "has moved past this commit" "$t/out"; then
  echo "FAIL nohint: the merge-main-in hint is printed for an ancestor base"; fails=$((fails + 1))
else
  echo "ok   nohint: no hint for an ancestor base"
fi

# FLOORS_BASE wins over BASE: b80 holds floor 80.0, b70 holds 70.0, the head is 75.0.
twobase() {
  repo "$1" '{"example.com/a":{"statements":80.0}}'
  git -C "$t/$1" branch b80
  echo '{"example.com/a":{"statements":70.0}}' >"$t/$1/coverage-floors.json"
  git -C "$t/$1" commit -q -am lower
  git -C "$t/$1" branch b70
  echo '{"example.com/a":{"statements":75.0}}' >"$t/$1/head.json"
}
twobase order
want order 1 "example.com/a: floor lowered 80.0" b80 BASE=b70
twobase orderrev
want orderrev 0 "floors-rise: base " b70 BASE=b80

# make ci-base's default BASE must not name a branch that can shadow the remote-tracking ref: refs/
# remotes/origin/main holds floor 80.0, a local branch origin/main 70.0, the head is 75.0. stale runs
# floors_rise.sh with the BASE the Makefile defaults to (read through a probe makefile); stalecibase
# and stalecihead run scripts/ci-base.sh, with a stub make, with BASE unset and BASE=HEAD.
repo stale '{"example.com/a":{"statements":80.0}}'
git -C "$t/stale" update-ref refs/remotes/origin/main HEAD
echo '{"example.com/a":{"statements":70.0}}' >"$t/stale/coverage-floors.json"
git -C "$t/stale" commit -q -am lower
git -C "$t/stale" branch origin/main
echo '{"example.com/a":{"statements":75.0}}' >"$t/stale/head.json"
printf 'probe-base:\n\t@echo $(BASE)\n' >"$t/probe.mk"
probe_base() {
  (cd "$root" && env -u BASE make -s -f Makefile -f "$t/probe.mk" probe-base)
}
mkbase=$(probe_base)
if [ -z "$mkbase" ]; then
  echo "FAIL stale: cannot read the Makefile's default BASE"; fails=$((fails + 1))
else
  want stale 1 "example.com/a: floor lowered 80.0" - "BASE=$mkbase"
fi
# A caller's make command-line variables (make scripts-test BASE=HEAD reaches here as MAKEFLAGS) must
# not change what the probe reads: the Makefile's default is wanted.
got_mf=$(MAKEFLAGS=' -- BASE=HEAD' MAKELEVEL=1 probe_base)
if [ "$got_mf" = "refs/remotes/origin/main" ]; then
  echo "ok   stalemf"
else
  echo "FAIL stalemf: the probe read '$got_mf' under MAKEFLAGS=' -- BASE=HEAD', want refs/remotes/origin/main"
  fails=$((fails + 1))
fi
# The ci-base recipe passes the Makefile's own BASE on to scripts/ci-base.sh, not a literal ref.
cibase_line=$(cd "$root" && env -u BASE -u MAKEFLAGS -u MFLAGS -u MAKEOVERRIDES -u MAKELEVEL make -n ci-base 2>&1 | grep -F "sh scripts/ci-base.sh")
if [ "$cibase_line" = "BASE=refs/remotes/origin/main sh scripts/ci-base.sh" ]; then
  echo "ok   cibaserecipe"
else
  echo "FAIL cibaserecipe: the ci-base recipe line is '$cibase_line', want BASE=refs/remotes/origin/main sh scripts/ci-base.sh"
  fails=$((fails + 1))
fi
mkdir -p "$t/bin" && printf '#!/bin/sh\nexit 0\n' >"$t/bin/make" && chmod +x "$t/bin/make"
cibase() {
  (cd "$t/stale" && env -u BASE PATH="$t/bin:$PATH" ${3:-} sh "$root/scripts/ci-base.sh") >"$t/out" 2>&1
  got=$?
  if [ "$got" != 0 ] || ! grep -qF -e "$2" "$t/out"; then
    echo "FAIL $1: exit $got, want 0 and a line containing '$2'"; sed 's/^/    /' "$t/out"; fails=$((fails + 1))
  else
    echo "ok   $1"
  fi
}
remote_sha=$(git -C "$t/stale" rev-parse refs/remotes/origin/main)
cibase stalecibase "base=$remote_sha (refs/remotes/origin/main)"
cibase stalecihead "(HEAD)" BASE=HEAD

# A git error reading the base is not "no floors file": a base whose tree object is gone fails closed.
repo gitfail '{"example.com/a":{"statements":80.0}}'
tree=$(git -C "$t/gitfail" rev-parse 'HEAD^{tree}')
rm -f "$t/gitfail/.git/objects/$(printf %s "$tree" | cut -c1-2)/$(printf %s "$tree" | cut -c3-)"
echo '{"example.com/a":{"statements":80.0}}' >"$t/gitfail/head.json"
want gitfail 2 "cannot read coverage-floors.json at base" HEAD

# A ref that is not a commit does not resolve.
repo treeref '{"example.com/a":{"statements":80.0}}'
echo '{"example.com/a":{"statements":80.0}}' >"$t/treeref/head.json"
want treeref 2 "cannot resolve" 'HEAD^{tree}'

# The default path (no COVERFLOORS) passes coverfloors' exit 2 through as 2, not go run's 1.
repo badjson '{"example.com/a":{"statements":80.0}}'
echo 'not json' >"$t/badjson/head.json"
cf=
want badjson 2 "coverfloors: " HEAD
cf="COVERFLOORS=$t/coverfloors"

# make floors runs floors-rise, so neither ci nor ci-base lists it again (ci-base.sh runs make floors).
for tgt in ci ci-base; do
  if grep -E "^$tgt:" "$root/Makefile" | grep -qw floors-rise; then
    echo "FAIL Makefile: $tgt lists floors-rise, which make floors already runs"; fails=$((fails + 1))
  else
    echo "ok   Makefile: $tgt does not list floors-rise"
  fi
done

# SIGTERM while coverfloors runs removes the script's temp dir. Run under dash: bash runs the EXIT
# trap on a signal by itself, dash does not, so only dash can show the missing trap.
repo sig '{"example.com/a":{"statements":80.0}}'
echo '{"example.com/a":{"statements":80.0}}' >"$t/sig/head.json"
# The stand-in coverfloors records the script's temp dir (the directory of its --base-floors file).
printf 'dirname "$3" >"%s/dir"\nsleep 2\n' "$t" >"$t/slow.sh"
if ! dash=$(command -v dash); then
  echo "skip sigterm: dash is not installed"
else
  (cd "$t/sig" && exec env FLOORS_BASE=HEAD FLOORS_FILE="$t/sig/head.json" COVERFLOORS="sh $t/slow.sh" \
    "$dash" "$root/scripts/floors_rise.sh") >"$t/sig.out" 2>&1 &
  pid=$!
  n=0
  while [ ! -s "$t/dir" ] && [ "$n" -lt 25 ]; do sleep 0.2; n=$((n + 1)); done
  if [ ! -s "$t/dir" ] || ! kill -TERM "$pid" 2>/dev/null; then
    echo "FAIL sigterm: the script ended before the signal"; sed 's/^/    /' "$t/sig.out"; fails=$((fails + 1))
  else
    wait "$pid"
    if [ -d "$(cat "$t/dir")" ]; then
      echo "FAIL sigterm: the temp dir is left behind"; fails=$((fails + 1))
    else
      echo "ok   sigterm"
    fi
  fi
fi

[ "$fails" = 0 ] && { echo "floors_rise_test: all cases pass"; exit 0; }
echo "floors_rise_test: $fails case(s) failed"
exit 1
