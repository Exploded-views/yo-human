#!/bin/sh
# release-targets.sh [FILE]: print the release targets as "GOOS GOARCH" lines, read from FILE
# (default: RELEASE_TARGETS at the repo root). Blank lines and # comments are skipped. Every
# target must be one `go tool dist list` knows, and there must be at least one.
# Exit: 0 printed, 2 missing, empty or unknown target.
set -u
file=${1:-$(cd "$(dirname "$0")/.." && pwd)/RELEASE_TARGETS}
[ -r "$file" ] || { echo "release-targets: cannot read RELEASE_TARGETS ($file)" >&2; exit 2; }
known=$(go tool dist list) || { echo "release-targets: go tool dist list failed" >&2; exit 2; }
n=0
while IFS= read -r line || [ -n "$line" ]; do
  line=${line%%#*}
  line=$(echo "$line" | tr -d ' \t\r')
  [ -z "$line" ] && continue
  if ! printf '%s\n' "$known" | grep -qxF -- "$line"; then
    echo "release-targets: $line in RELEASE_TARGETS is not a target go knows" >&2; exit 2
  fi
  echo "${line%/*} ${line#*/}"
  n=$((n + 1))
done <"$file"
[ "$n" -gt 0 ] || { echo "release-targets: RELEASE_TARGETS lists no target" >&2; exit 2; }
