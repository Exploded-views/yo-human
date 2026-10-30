#!/bin/sh
# Usage: scripts/nohooks.sh <binary-or-.tar.gz>...
# A release binary must hold neither test-only hook name (YO_HUMAN_TEST_NOW, YO_HUMAN_TEST_CA);
# both exist only under -tags e2e. An archive is unpacked first. As a control that the search is
# not vacuous, each binary must hold YO_HUMAN_CONFIG, a variable a release build does read.
set -eu

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# count PATTERN FILE: the number of lines of FILE matching PATTERN. Nothing matching (grep exit 1)
# is 0; grep failing (exit 2) stops the script with exit 2, so a failure never passes as no match.
count() {
	n=$(grep -cE -- "$1" "$2") || [ $? -eq 1 ] || { echo "nohooks: grep failed on $2" >&2; exit 2; }
	echo "$n"
}

check() {
	strings "$1" >"$tmp/strings" # a failing strings stops the script (set -e)
	hooks=$(count 'YO_HUMAN_TEST_(NOW|CA)' "$tmp/strings")
	control=$(count 'YO_HUMAN_CONFIG' "$tmp/strings")
	if [ "$control" -lt 1 ]; then
		echo "nohooks: $2: the control string YO_HUMAN_CONFIG is missing, so the search proves nothing" >&2
		exit 1
	fi
	if [ "$hooks" -ne 0 ]; then
		echo "nohooks: $2 holds a test-only hook name ($hooks strings)" >&2
		exit 1
	fi
	echo "nohooks: $2 holds no test-only hook name"
}

n=0
for f in "$@"; do
	case "$f" in
	*.tar.gz)
		n=$((n + 1))
		mkdir "$tmp/$n"
		tar -xzf "$f" -C "$tmp/$n" yo-human
		check "$tmp/$n/yo-human" "$f"
		;;
	*)
		check "$f" "$f"
		;;
	esac
done
test "$#" -gt 0 || { echo "usage: scripts/nohooks.sh <binary-or-.tar.gz>..." >&2; exit 64; }
