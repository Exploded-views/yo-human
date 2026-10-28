#!/bin/sh
# hooks_test.sh: the git hooks from this lefthook.yml, end to end. It copies the tracked files of
# this work tree (as they are on disk, so uncommitted hook changes are what is tested) into a
# scratch repo under a temp dir, commits them, installs lefthook there, and checks:
# - a commit that adds a deliberately failing test is accepted by pre-commit (a RED commit
#   passes the hooks like any other commit);
# - a commit with a gofmt violation is refused;
# - a commit with an _e2e.go file that lacks the e2e build tag is refused;
# - a commit that stages a key generated at run time is refused, and the key is never printed;
# - a push of a branch whose tests fail is refused by pre-push, and a push after the fix is
#   accepted.
# Never --no-verify. The temp dir is removed on every exit.
# Exit: 0 all cases pass, 1 a case failed, 2 setup error (lefthook missing, copy or git failure).
set -u
root=$(git rev-parse --show-toplevel) || exit 2
command -v lefthook >/dev/null 2>&1 || { echo "hooks_test: lefthook is not installed; run: make tools"; exit 2; }
t=$(mktemp -d) || exit 2
trap 'rm -rf "$t"' EXIT
trap 'exit 2' INT TERM
GIT_CEILING_DIRECTORIES=$t
export GIT_CEILING_DIRECTORIES GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.com
export GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.com NO_COLOR=1
unset LEFTHOOK LEFTHOOK_EXCLUDE GITLEAKS_EXTRA
fails=0
esc=$(printf '\033')

rnd() { LC_ALL=C tr -dc 'A-Z2-7' </dev/urandom | head -c "$1"; }

# case NAME WANT_EXIT CMD...: run CMD in the scratch repo, compare the exit code; output in $t/out.
case_() {
  name=$1 want=$2
  shift 2
  "$@" >"$t/out" 2>&1
  got=$?
  if { [ "$want" = 0 ] && [ "$got" != 0 ]; } || { [ "$want" != 0 ] && [ "$got" = 0 ]; }; then
    echo "FAIL $name: want exit $want, got $got"; sed -n '1,40p' "$t/out" | sed "s#$t#\$TMP#g; s/$esc\[[0-9;]*m//g; s/^/     /"
    fails=$((fails + 1)); return 1
  fi
  echo "ok   $name (exit $got)"
}

# expect NAME TEXT: assert the last output contains TEXT.
expect() {
  if ! grep -q -- "$2" "$t/out"; then echo "FAIL $1: output lacks '$2'"; fails=$((fails + 1)); fi
}

mkdir "$t/repo" "$t/remote.git" || exit 2
(cd "$root" && git ls-files -z | xargs -0 tar -cf -) | (cd "$t/repo" && tar -xf -) || exit 2
git init -q --bare "$t/remote.git" || exit 2
cd "$t/repo" || exit 2
{ git init -q . && git add -A && git commit -q -m base && git remote add origin "$t/remote.git" \
  && git push -q origin HEAD:refs/heads/main 2>/dev/null; } || { echo "hooks_test: scratch repo setup failed"; exit 2; }
lefthook install >"$t/install.log" 2>&1 || { echo "hooks_test: lefthook install failed"; cat "$t/install.log"; exit 2; }

# A RED commit: a test that fails on purpose. Pre-commit runs no tests, so it is accepted.
mkdir -p internal/hookred
printf 'package hookred\n' >internal/hookred/doc.go
printf 'package hookred\n\nimport "testing"\n\nfunc TestRed(t *testing.T) { t.Fatal("red on purpose") }\n' \
  >internal/hookred/red_test.go
git add internal/hookred
case_ "pre-commit accepts a RED commit (a failing test)" 0 git commit -m "test: red on purpose (RED)"

# gofmt: a badly formatted file is refused, and gofmt names it.
printf 'package hookred\nfunc  Bad( ) {}\n' >internal/hookred/bad.go
git add internal/hookred/bad.go
case_ "pre-commit refuses a gofmt violation" 1 git commit -m "chore: bad format" \
  && expect "gofmt names the file" "internal/hookred/bad.go"
git rm -q --cached internal/hookred/bad.go && rm -f internal/hookred/bad.go

# The e2e build-tag check: an untagged _e2e.go file is refused.
printf 'package hookred\n\nimport _ "net/http"\n' >internal/hookred/x_e2e.go
git add internal/hookred/x_e2e.go
case_ "pre-commit refuses an _e2e.go file without the e2e tag" 1 git commit -m "chore: untagged" \
  && expect "the check names the file" "internal/hookred/x_e2e.go"
git rm -q --cached internal/hookred/x_e2e.go && rm -f internal/hookred/x_e2e.go

# A staged secret: a key generated now, never kept; refused, and never printed.
printf 'aws_access_key_id = AKIA%s\n' "$(rnd 16)" >leak.env
git add leak.env
case_ "pre-commit refuses a staged secret" 1 git commit -m "chore: leak" \
  && expect "the finding names the rule and file" "aws-access-token leak.env"
n=$(grep -c AKIA "$t/out")
[ "$n" = 0 ] || { echo "FAIL the key prefix appears $n times in the hook output"; fails=$((fails + 1)); }
git rm -q --cached leak.env && rm -f leak.env

# Pre-push runs the tests: the RED tip is refused; after the fix it is accepted.
case_ "pre-push refuses a branch whose tests fail" 1 git push origin HEAD:refs/heads/red \
  && expect "the failing test is named" "TestRed"
printf 'package hookred\n\nimport "testing"\n\nfunc TestRed(t *testing.T) {}\n' >internal/hookred/red_test.go
git add internal/hookred/red_test.go
case_ "pre-commit accepts the fix" 0 git commit -m "fix: green"
case_ "pre-push accepts a branch whose tests pass" 0 git push origin HEAD:refs/heads/red

[ "$fails" = 0 ] || { echo "hooks_test: $fails failure(s)"; exit 1; }
echo "hooks_test: all cases pass"
