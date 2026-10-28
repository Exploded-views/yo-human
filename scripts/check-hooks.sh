#!/bin/sh
# check-hooks.sh: fail unless lefthook's pre-commit and pre-push hooks are both installed in
# this clone, so local commits run the fast gates and pushes run the tests. Skipped in CI
# (CI=true), which runs every gate itself.
# Exit: 0 installed (or CI), 1 missing; the fix is `make hooks`.
set -u
if [ "${CI:-}" = true ]; then
  echo "check-hooks: skipped (CI)"
  exit 0
fi
missing=0
for name in pre-commit pre-push; do
  hook=$(git rev-parse --git-path "hooks/$name") || exit 1
  if [ -x "$hook" ] && grep -q call_lefthook "$hook"; then
    echo "check-hooks: lefthook $name hook installed"
  else
    echo "check-hooks: lefthook's $name hook is not installed; run: make hooks" >&2
    missing=1
  fi
done
exit "$missing"
