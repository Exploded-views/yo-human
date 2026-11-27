#!/bin/sh
# floors_rise.sh: coverage floors only rise. Compares coverage-floors.json (FLOORS_FILE) with the
# copy at the base itself, with no coverage run, and fails when a floor is lower or removed or an
# exemption is added (coverfloors --rise-only). The base is FLOORS_BASE, else BASE (as make ci-base
# passes it), else refs/remotes/origin/main (not origin/main: a local branch of that name would
# shadow it). A base commit with no floors file is skipped, as in CI; a git error reading it is not.
# COVERFLOORS is the coverfloors command (default: built from this repo into a temp dir).
# Exit: 0 clean or skipped, 1 a floor fell, 2 the ref does not resolve, git cannot read the base or
# an input is unreadable (fails closed).
set -eu
ref=${FLOORS_BASE:-${BASE:-refs/remotes/origin/main}}
file=${FLOORS_FILE:-coverage-floors.json}
root=$(cd "$(dirname "$0")/.." && pwd)
if ! base=$(git rev-parse --verify --quiet "$ref^{commit}"); then
  echo "floors-rise: cannot resolve $ref; fetch it or pass FLOORS_BASE=<commit>" >&2
  exit 2
fi
echo "floors-rise: base $base ($ref)"
if ! entry=$(git ls-tree --full-tree "$base" -- coverage-floors.json); then
  echo "floors-rise: cannot read coverage-floors.json at base $base" >&2
  exit 2
fi
case $entry in
"")
  echo "floors-rise: skipped, no coverage-floors.json at base"
  exit 0
  ;;
*" blob "*) ;;
*)
  echo "floors-rise: coverage-floors.json at base $base is not a file" >&2
  exit 2
  ;;
esac
d=$(mktemp -d)
trap 'rm -rf "$d"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
cmd=${COVERFLOORS:-}
if [ -z "$cmd" ]; then
  # go run would report the tool's exit 2 as its own 1; the built binary passes 2 through.
  (cd "$root" && go build -o "$d/coverfloors" ./tools/coverfloors) || { echo "floors-rise: cannot build coverfloors" >&2; exit 2; }
  cmd="$d/coverfloors"
fi
git show "$base:coverage-floors.json" > "$d/base-floors.json"
rc=0
$cmd --rise-only --base-floors "$d/base-floors.json" --floors "$file" || rc=$?
if [ "$rc" = 1 ] && ! git merge-base --is-ancestor "$base" HEAD; then
  echo "floors-rise: $ref has moved past this commit; merge main in and re-run" >&2
fi
exit "$rc"
