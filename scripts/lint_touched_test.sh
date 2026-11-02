#!/bin/sh
# lint_touched_test.sh: scripts/lint-touched.sh lints each Go package a push touches, once, passes
# lint.sh's exit code through, and skips lint.sh when no Go package is left. Each case runs in a
# scratch tree: a copy of the script, a fake lint.sh that records its arguments, and some files.
# Exit: 0 all cases pass, 1 a case failed.
set -u
root=$(cd "$(dirname "$0")/.." && pwd)
t=$(mktemp -d) || exit 1
trap 'rm -rf "$t"' EXIT
fails=0

if [ ! -f "$root/scripts/lint-touched.sh" ]; then
  echo "FAIL scripts/lint-touched.sh is missing"
  echo "lint_touched_test: 1 case(s) failed"
  exit 1
fi

# tree NAME: a scratch tree $t/NAME with Go files in pkg/a and pkg/b, a Markdown file in docs/,
# and scripts/ holding a copy of lint-touched.sh and a fake lint.sh. The fake writes its
# arguments, one per line, to $t/NAME/args and exits with $FAKE_EXIT (default 0).
tree() {
  mkdir -p "$t/$1/scripts" "$t/$1/pkg/a" "$t/$1/pkg/b" "$t/$1/docs" || exit 1
  touch "$t/$1/pkg/a/x.go" "$t/$1/pkg/a/y.go" "$t/$1/pkg/b/z.go" "$t/$1/docs/readme.md" || exit 1
  cp "$root/scripts/lint-touched.sh" "$t/$1/scripts/" || exit 1
  cat >"$t/$1/scripts/lint.sh" <<EOF
#!/bin/sh
printf '%s\n' "\$@" >"$t/$1/args"
exit "\${FAKE_EXIT:-0}"
EOF
}

# want NAME CODE DIR CMD...: runs CMD in DIR, fails the case unless it exits CODE. Output: $t/out.
want() {
  name=$1 code=$2 dir=$3; shift 3
  (cd "$dir" && "$@") >"$t/out" 2>&1
  got=$?
  if [ "$got" != "$code" ]; then
    echo "FAIL $name: exit $got, want $code"; sed 's/^/    /' "$t/out"; fails=$((fails + 1))
  else
    echo "ok   $name"
  fi
}

# packages NAME: the sorted lint.sh arguments of tree NAME, joined by spaces (empty if lint.sh
# never ran).
packages() { sort "$t/$1/args" 2>/dev/null | tr '\n' ' '; }

# skipped NAME: the run printed the no-packages message and never called lint.sh.
skipped() {
  grep -q '^lint-touched: no Go packages touched$' "$t/out" || { echo "FAIL $1: no 'no Go packages touched' message"; fails=$((fails + 1)); }
  [ -e "$t/$2/args" ] && { echo "FAIL $1: lint.sh was called"; fails=$((fails + 1)); }
  return 0
}

tree two
want "two Go files in one directory and one in another" 0 "$t/two" sh scripts/lint-touched.sh pkg/a/x.go pkg/a/y.go pkg/b/z.go
[ "$(packages two)" = "./pkg/a ./pkg/b " ] || { echo "FAIL packages linted: '$(packages two)', want './pkg/a ./pkg/b' once each"; fails=$((fails + 1)); }

tree none
want "a Markdown file and a deleted directory's Go file are ignored" 0 "$t/none" sh scripts/lint-touched.sh docs/readme.md gone/w.go
skipped "no Go package left" none

tree empty
want "no file arguments" 0 "$t/empty" sh scripts/lint-touched.sh
skipped "no file arguments" empty

tree fail
want "lint.sh's exit code 1 is passed through" 1 "$t/fail" env FAKE_EXIT=1 sh scripts/lint-touched.sh pkg/a/x.go

[ "$fails" = 0 ] && { echo "lint_touched_test: all cases pass"; exit 0; }
echo "lint_touched_test: $fails case(s) failed"
exit 1
