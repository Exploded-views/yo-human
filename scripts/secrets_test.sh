#!/bin/sh
# secrets_test.sh: tests for scripts/secrets.sh on scratch repos built under a temp dir.
# Keys are generated at run time inside the temp dir and are never written to a kept file.
# Exit: 0 all cases pass, 1 a case failed.
set -u
unset GITLEAKS_EXTRA
script=$(cd "$(dirname "$0")" && pwd)/secrets.sh
t=$(mktemp -d) || exit 1
trap 'rm -rf "$t"' EXIT
GIT_CEILING_DIRECTORIES=$t
export GIT_CEILING_DIRECTORIES GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.com
export GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.com
fails=0

rnd() { LC_ALL=C tr -dc 'A-Z2-7' </dev/urandom | head -c "$1"; }

# newrepo NAME: a scratch repo with one unrelated file committed; leaves cwd inside it.
newrepo() {
  mkdir "$t/$1" && cd "$t/$1" || exit 1
  git init -q . && echo unrelated >other.txt && git add other.txt && git commit -q -m init
}

# check NAME WANT_EXIT [ARG...]: run the script in cwd with ARGs, compare the exit code, keep
# output in $t/out.
check() {
  name=$1 want=$2
  shift 2
  sh "$script" "$@" >"$t/out" 2>&1
  got=$?
  if [ "$got" != "$want" ]; then
    echo "FAIL $name: want exit $want, got $got"; fails=$((fails + 1)); return 1
  fi
  echo "ok   $name (exit $got)"
}

# expect NAME TEXT: assert the last output contains TEXT.
expect() {
  if ! grep -q -- "$2" "$t/out"; then echo "FAIL $1: output lacks '$2'"; fails=$((fails + 1)); fi
}

newrepo clean
check clean 0 && expect clean "secrets: 0 findings"

newrepo shape
git checkout -q -b side && echo side >side.txt && git add side.txt && git commit -q -m side
git checkout -q - && echo main >main.txt && git add main.txt && git commit -q -m main
git merge -q --no-edit side && git commit -q --allow-empty -m empty
check merge-and-empty-commits 0 && expect merge-and-empty-commits "secrets: 0 findings"

# noleak NAME: assert the last output holds no AKIA key prefix. grep's own failure (exit 2) fails
# the case too, rather than passing as no match.
noleak() {
  grep -q AKIA "$t/out"
  case $? in
  1) ;;
  0) echo "FAIL $1: AKIA appears in output"; fails=$((fails + 1)) ;;
  *) echo "FAIL $1: grep could not read the output"; fails=$((fails + 1)) ;;
  esac
}

newrepo leak
printf 'aws_access_key_id = AKIA%s\n' "$(rnd 16)" >leak.env
git add leak.env && git commit -q -m leak
check leak 1 && {
  expect leak "secrets: 1 findings"; expect leak "aws-access-token leak.env "
  noleak leak
}

# the same repo with an unrelated blob gone: gitleaks logs an error and scans nothing.
blob=$(git rev-parse HEAD:other.txt)
rm -f ".git/objects/$(echo "$blob" | cut -c1-2)/$(echo "$blob" | cut -c3-)"
check missing-object 2

mkdir "$t/norepo" && cd "$t/norepo" || exit 1
check not-a-repo 2

newrepo extra
GITLEAKS_EXTRA=$t/does-not-exist.toml
export GITLEAKS_EXTRA
check unreadable-extra 2
unset GITLEAKS_EXTRA

# Repos with init plus one commit of a shape that adds no lines: gitleaks counts none for it.
newrepo shape-delete
git rm -q other.txt && git commit -q -m delete
check shape-delete-only 0

newrepo shape-binary
printf '\000\001\002\377' >bin.dat && git add bin.dat && git commit -q -m binary
check shape-binary-only 0

newrepo shape-rename
git mv other.txt moved.txt && git commit -q -m rename
check shape-pure-rename 0

newrepo shape-mode
chmod +x other.txt && git add other.txt && git commit -q -m mode
check shape-mode-only 0

newrepo shape-emptyfile
: >empty.txt && git add empty.txt && git commit -q -m emptyfile
check shape-empty-file 0

# Object-database damage. missing-middle-commit is caught by git rev-list or by gitleaks scanning
# no commits; missing-root-blob by gitleaks scanning no commits. missing-merge-only-blob is
# caught by the fsck gate (and, since merges are scanned against their first parent, by gitleaks
# logging an error): a blob that only a merge commit introduces is never read by rev-list.
rmobj() { rm -f ".git/objects/$(echo "$1" | cut -c1-2)/$(echo "$1" | cut -c3-)"; }
newrepo miss-mid
echo two >two.txt && git add two.txt && git commit -q -m two
echo three >three.txt && git add three.txt && git commit -q -m three
rmobj "$(git rev-parse HEAD~1)"
check missing-middle-commit 2

newrepo miss-root
echo two >two.txt && git add two.txt && git commit -q -m two
echo three >three.txt && git add three.txt && git commit -q -m three
rmobj "$(git rev-parse "$(git rev-list --max-parents=0 HEAD):other.txt")"
check missing-root-blob 2

newrepo miss-merge-blob
git checkout -q -b side && echo side >side.txt && git add side.txt && git commit -q -m side
git checkout -q - && echo main >main.txt && git add main.txt && git commit -q -m main
git merge -q --no-commit --no-ff side >/dev/null 2>&1 && echo only-in-merge >merged.txt && git add merged.txt
git commit -q -m merge
echo later >later.txt && git add later.txt && git commit -q -m later
rmobj "$(git rev-parse HEAD~1:merged.txt)"
check missing-merge-only-blob 2

newrepo fake
printf 'secret_key = "R0UTING-KEY-FAKE"\n' >fixture.txt
git add fixture.txt && git commit -q -m fixture
check fake-word 0

newrepo fake-nohyphen
printf 'aws_access_key_id = AKIA%sFAKE%s\n' "$(rnd 8)" "$(rnd 4)" >leak.env
git add leak.env && git commit -q -m leak
check fake-embedded 1 && expect fake-embedded "aws-access-token leak.env "


# A key that only a merge commit adds (an evil merge): no parent commit introduces it.
newrepo evil
git checkout -q -b side && echo side >side.txt && git add side.txt && git commit -q -m side
git checkout -q - && echo main >main.txt && git add main.txt && git commit -q -m main
git merge -q --no-commit --no-ff side >/dev/null 2>&1
printf 'aws_access_key_id = AKIA%s\n' "$(rnd 16)" >leak.env
git add leak.env && git commit -q -m merge
check evil-merge 1 && { expect evil-merge "aws-access-token leak.env "; noleak evil-merge; }

# An inline gitleaks:allow comment must not hide a real key.
newrepo inline
printf 'aws_access_key_id = AKIA%s # gitleaks:allow\n' "$(rnd 16)" >leak.env
git add leak.env && git commit -q -m leak
check inline-allow 1 && { expect inline-allow "aws-access-token leak.env "; noleak inline-allow; }

# A committed .gitleaksignore naming the finding's fingerprint must not hide it.
newrepo ignorefile
printf 'aws_access_key_id = AKIA%s\n' "$(rnd 16)" >leak.env
git add leak.env && git commit -q -m leak
printf '%s:leak.env:aws-access-token:1\n' "$(git rev-parse HEAD)" >.gitleaksignore
git add .gitleaksignore && git commit -q -m ignore
check gitleaksignore-file 1 && { expect gitleaksignore-file "aws-access-token leak.env "; noleak gitleaksignore-file; }

# --staged (the pre-commit hook): only the staged diff is scanned, with the same safeguards.
newrepo staged-clean
echo hello >a.txt && git add a.txt
check staged-clean 0 --staged && expect staged-clean "secrets (staged): 0 findings"

newrepo staged-leak
printf 'aws_access_key_id = AKIA%s\n' "$(rnd 16)" >leak.env && git add leak.env
check staged-leak 1 --staged && { expect staged-leak "aws-access-token leak.env "; noleak staged-leak; }

newrepo staged-unstaged
printf 'aws_access_key_id = AKIA%s\n' "$(rnd 16)" >leak.env
echo hello >a.txt && git add a.txt
check staged-ignores-unstaged 0 --staged

newrepo staged-inline
printf 'aws_access_key_id = AKIA%s # gitleaks:allow\n' "$(rnd 16)" >leak.env && git add leak.env
check staged-inline-allow 1 --staged && noleak staged-inline-allow

newrepo staged-ignorefile
printf 'aws_access_key_id = AKIA%s\n' "$(rnd 16)" >leak.env && git add leak.env
printf 'leak.env:aws-access-token:1\n' >.gitleaksignore
check staged-gitleaksignore 1 --staged && noleak staged-gitleaksignore

# The first commit of a new repo: there is no HEAD yet.
mkdir "$t/staged-first" && cd "$t/staged-first" && git init -q . || exit 1
printf 'aws_access_key_id = AKIA%s\n' "$(rnd 16)" >leak.env && git add leak.env
check staged-first-commit 1 --staged && noleak staged-first-commit

# Inside a hook, git can hand the script relative GIT_DIR and GIT_INDEX_FILE paths.
newrepo staged-hookenv
printf 'aws_access_key_id = AKIA%s\n' "$(rnd 16)" >leak.env && git add leak.env
GIT_DIR=.git GIT_INDEX_FILE=.git/index
export GIT_DIR GIT_INDEX_FILE
check staged-relative-hook-env 1 --staged && noleak staged-relative-hook-env
unset GIT_DIR GIT_INDEX_FILE

newrepo staged-fake
printf 'secret_key = "R0UTING-KEY-FAKE"\n' >fixture.txt && git add fixture.txt
check staged-fake-word 0 --staged

# Each token the bus mints (node, OAuth access, refresh, desk starter, desk human, executor) is
# caught by the yo-human-token rule by name, and the token never appears in the output.
for k in n a r d h x; do
  newrepo "token-yh$k"
  printf 'BUS_TOKEN=yh%s_%s\n' "$k" "$(LC_ALL=C tr -dc 'a-z2-7' </dev/urandom | head -c 52)" >bus.env
  git add bus.env && git commit -q -m token
  check "token-yh$k" 1 && {
    expect "token-yh$k" "yo-human-token bus.env "
    n=$(grep -c "yh${k}_" "$t/out")
    [ "$n" = 0 ] || { echo "FAIL token-yh$k: the token appears $n times in output"; fails=$((fails + 1)); }
  }
done

# Not tokens: an unknown prefix (yhq_) and a known one a character short. No word that the
# generic rule keys on, so a finding here is the yo-human-token rule overreaching.
for c in "yhq_:52" "yhn_:51"; do
  newrepo "not-token-${c%%:*}${c#*:}"
  printf 'ref %s%s\n' "${c%%:*}" "$(LC_ALL=C tr -dc 'a-z2-7' </dev/urandom | head -c "${c#*:}")" >notes.txt
  git add notes.txt && git commit -q -m note
  check "not-token-${c%%:*}${c#*:}" 0
done

newrepo bad-flag
check unknown-flag 2 --bogus

[ "$fails" = 0 ] || { echo "secrets_test: $fails failure(s)"; exit 1; }
echo "secrets_test: all cases pass"
