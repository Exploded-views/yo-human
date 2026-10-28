#!/bin/sh
# check_hooks_test.sh: tests for scripts/check-hooks.sh on scratch repos built under a temp dir.
# Exit: 0 all cases pass, 1 a case failed.
set -u
script=$(cd "$(dirname "$0")" && pwd)/check-hooks.sh
t=$(mktemp -d) || exit 1
trap 'rm -rf "$t"' EXIT
GIT_CEILING_DIRECTORIES=$t
export GIT_CEILING_DIRECTORIES
fails=0

# check NAME WANT_EXIT [ENV...]: run the script in cwd with the given env, compare the exit code.
check() {
  name=$1 want=$2
  shift 2
  env "$@" sh "$script" >"$t/out" 2>&1
  got=$?
  if [ "$got" != "$want" ]; then
    echo "FAIL $name: want exit $want, got $got"; sed 's/^/     /' "$t/out"; fails=$((fails + 1)); return
  fi
  echo "ok   $name (exit $got)"
}

mkdir "$t/repo" && cd "$t/repo" && git init -q . || exit 1
hook=$(git rev-parse --git-path hooks/pre-commit)
push=$(git rev-parse --git-path hooks/pre-push)

# lefthook NAME FILE: write lefthook's hook for NAME to FILE, executable.
lefthook() { printf '#!/bin/sh\ncall_lefthook run "%s" "$@"\n' "$1" >"$2" && chmod +x "$2"; }

check "no hook installed" 1 CI=
printf '#!/bin/sh\necho some other hook\n' >"$hook" && chmod +x "$hook"
lefthook pre-push "$push"
check "a pre-commit hook that is not lefthook's" 1 CI=
lefthook pre-commit "$hook"
chmod -x "$hook"
check "lefthook's pre-commit, not executable" 1 CI=
chmod +x "$hook"
check "both lefthook hooks installed" 0 CI=
rm -f "$push"
check "pre-commit only, no pre-push" 1 CI=
printf '#!/bin/sh\necho some other hook\n' >"$push" && chmod +x "$push"
check "a pre-push hook that is not lefthook's" 1 CI=
lefthook pre-push "$push" && chmod -x "$push"
check "lefthook's pre-push, not executable" 1 CI=
rm -f "$hook" "$push"
check "skipped in CI" 0 CI=true

if [ "$fails" -ne 0 ]; then
  echo "check_hooks_test: $fails case(s) failed"; exit 1
fi
echo "check_hooks_test: all cases pass"
