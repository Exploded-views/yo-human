#!/bin/sh
# floors_rise.sh: coverage floors only rise. Compares coverage-floors.json (FLOORS_FILE) with the
# copy at ${FLOORS_BASE:-origin/main} itself, with no coverage run, and fails when a floor is lower
# or removed or an exemption is added (coverfloors --rise-only). A base commit with no floors file
# is skipped, as in CI. COVERFLOORS is the coverfloors command (default: go run it from this repo).
# Exit: 0 clean or skipped, 1 a floor fell, 2 the ref does not resolve or an input is unreadable
# (fails closed).
set -eu
ref=${FLOORS_BASE:-origin/main}
file=${FLOORS_FILE:-coverage-floors.json}
cmd=${COVERFLOORS:-go run ./tools/coverfloors}
if ! base=$(git rev-parse --verify --quiet "$ref^{commit}"); then
  echo "floors-rise: cannot resolve $ref; fetch it or pass FLOORS_BASE=<commit>" >&2
  exit 2
fi
echo "floors-rise: base $base ($ref)"
if ! git cat-file -e "$base:coverage-floors.json" 2>/dev/null; then
  echo "floors-rise: skipped, no coverage-floors.json at base"
  exit 0
fi
d=$(mktemp -d)
trap 'rm -rf "$d"' EXIT
git show "$base:coverage-floors.json" > "$d/base-floors.json"
rc=0
$cmd --rise-only --base-floors "$d/base-floors.json" --floors "$file" || rc=$?
exit "$rc"
