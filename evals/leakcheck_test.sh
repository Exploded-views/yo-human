#!/usr/bin/env bash
# leakcheck_test.sh: plant each kind of leak in a temp dir and check leakcheck.sh flags it, and
# stays quiet on placeholders. Prints one line per kind.
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
fail=0
check() { # name text want(0 clean|1 leak)
  rm -f "$tmp"/*; printf '%s\n' "$2" > "$tmp/case.txt"
  "$here/leakcheck.sh" "$tmp" > /dev/null; got=$?
  if (( got == $3 )); then echo "ok   $1"; else echo "FAIL $1 (exit $got)"; fail=1; fi
}
check ulid "id 01ARZ3NDEKTSV4RRFFQ69G5FAV" 1
check timestamp "at 2026-10-26T10:11:12Z" 1
# The plants below are built at runtime (shell concatenation) so the public-repo scrub stays uniform.
check session-uuid "123e4567-e89b-12d3-a456-""426614174000" 1
check key "ghp_abcdefghijklmnop1234" 1
check long-token "abcdefghijklmnopqrstuvwxyz0123456789" 1
check users-path "/Us""ers/someone/x" 1
check home-path "/ho""me/someone/x" 1
check macos-temp "in $TMPDIR" 1
check macos-private-temp "at /private/var/folders/q/T" 1
check ipv4 "host 10.1.""2.3" 1
check email "mail a.b@""corp.org" 1
check hostname "db1.corp.""internal" 1
check clean "example.com, page.sh, FAKE-TOKEN-0000 and levels --json" 0
exit $fail
