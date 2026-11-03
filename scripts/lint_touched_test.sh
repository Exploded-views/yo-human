#!/bin/sh
# lint_touched_test.sh: scripts/lint-touched.sh lints each Go package a push touches. A package with
# no file left under the default build tags goes only to lint.sh's e2e pass (--host-only, with the
# e2e tag); any other goes to lint.sh and to that pass. The worst exit code of the passes is the
# exit code, and no lint.sh runs when no Go package is left. Each case runs in a scratch tree: a
# copy of the script, a fake lint.sh that records its arguments, a fake go, and some files.
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

# The fake go answers the file counts of `go list -e -f ...`: a directory named pkg/tag has only
# tagged files (zeros), any other has one plain file.
mkdir -p "$t/bin" || exit 1
cat >"$t/bin/go" <<'EOF'
#!/bin/sh
case "$*" in *pkg/tag*) echo "0 0 0" ;; *) echo "1 0 0" ;; esac
EOF
chmod +x "$t/bin/go" || exit 1
PATH="$t/bin:$PATH"
export PATH

# tree NAME: a scratch tree $t/NAME with Go files in pkg/a, pkg/b and pkg/tag (tagged only), a
# Markdown file in docs/, and scripts/ holding a copy of lint-touched.sh and a fake lint.sh. The
# fake writes its arguments, one per line, to $t/NAME/plain, or to $t/NAME/host for --host-only,
# and exits with $FAKE_EXIT, or $FAKE_HOST_EXIT for the host pass (both default 0).
tree() {
  mkdir -p "$t/$1/scripts" "$t/$1/pkg/a" "$t/$1/pkg/b" "$t/$1/pkg/tag" "$t/$1/docs" || exit 1
  touch "$t/$1/pkg/a/x.go" "$t/$1/pkg/a/y.go" "$t/$1/pkg/b/z.go" "$t/$1/pkg/tag/e2e_test.go" \
    "$t/$1/docs/readme.md" || exit 1
  # Directories the go tool ignores, and one with a space in its name, all present on disk.
  mkdir -p "$t/$1/pkg/a/testdata" "$t/$1/pkg/_x" "$t/$1/pkg/.x" "$t/$1/pkg/a b" "$t/$1/emptydir" || exit 1
  touch "$t/$1/pkg/a/testdata/f.go" "$t/$1/pkg/_x/f.go" "$t/$1/pkg/.x/f.go" "$t/$1/pkg/a b/s.go" \
    "$t/$1/emptydir/readme.md" || exit 1
  cp "$root/scripts/lint-touched.sh" "$t/$1/scripts/" || exit 1
  cat >"$t/$1/scripts/lint.sh" <<EOF
#!/bin/sh
case "\${1-}" in --host-only) mode=host; code=\${FAKE_HOST_EXIT:-0} ;; *) mode=plain; code=\${FAKE_EXIT:-0} ;; esac
printf '%s\n' "\$@" >"$t/$1/\$mode"
exit "\$code"
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

# pkgs NAME MODE: the sorted package arguments (lines starting ./) of the MODE pass of tree NAME,
# joined by spaces; empty if that pass never ran.
pkgs() { grep '^\./' "$t/$1/$2" 2>/dev/null | sort | tr '\n' ' '; }

# expect NAME WHAT GOT WANT: a failed check prints the case and what was seen.
expect() {
  [ "$3" = "$4" ] || { echo "FAIL $1: $2 is '$3', want '$4'"; fails=$((fails + 1)); }
}

# skipped NAME TREE: the run printed the no-packages message and never called lint.sh.
skipped() {
  grep -q '^lint-touched: no Go packages touched$' "$t/out" || { echo "FAIL $1: no 'no Go packages touched' message"; fails=$((fails + 1)); }
  { [ -e "$t/$2/plain" ] || [ -e "$t/$2/host" ]; } && { echo "FAIL $1: lint.sh was called"; fails=$((fails + 1)); }
  return 0
}

tree two
want "plain packages in one directory and another" 0 "$t/two" sh scripts/lint-touched.sh pkg/a/x.go pkg/a/y.go pkg/b/z.go
expect "plain packages, plain pass" "packages" "$(pkgs two plain)" "./pkg/a ./pkg/b "
expect "plain packages, e2e pass" "packages" "$(pkgs two host)" "./pkg/a ./pkg/b "
expect "e2e pass flags" "its first arguments" "$(sed -n 1,3p "$t/two/host" | tr '\n' ' ')" "--host-only --build-tags e2e "

tree tg
want "a tagged-only package goes only to the e2e pass" 0 "$t/tg" sh scripts/lint-touched.sh pkg/tag/e2e_test.go
[ -e "$t/tg/plain" ] && { echo "FAIL tagged-only: lint.sh ran without the e2e pass"; fails=$((fails + 1)); }
expect "tagged-only, e2e pass" "packages" "$(pkgs tg host)" "./pkg/tag "

tree mix
want "a plain and a tagged-only package" 0 "$t/mix" sh scripts/lint-touched.sh pkg/a/x.go pkg/tag/e2e_test.go
expect "mixed, plain pass" "packages" "$(pkgs mix plain)" "./pkg/a "
expect "mixed, e2e pass" "packages" "$(pkgs mix host)" "./pkg/a ./pkg/tag "

tree none
want "a Markdown file and a deleted directory's Go file are ignored" 0 "$t/none" sh scripts/lint-touched.sh docs/readme.md gone/w.go
skipped "no Go package left" none

tree empty
want "no file arguments" 0 "$t/empty" sh scripts/lint-touched.sh
skipped "no file arguments" empty

tree fail
want "a plain pass exit 1 is passed through" 1 "$t/fail" env FAKE_EXIT=1 sh scripts/lint-touched.sh pkg/a/x.go

tree w1
want "plain pass 0, e2e pass 1: the worst is 1" 1 "$t/w1" env FAKE_EXIT=0 FAKE_HOST_EXIT=1 sh scripts/lint-touched.sh pkg/a/x.go

tree w2
want "plain pass 2, e2e pass 0: the worst is 2" 2 "$t/w2" env FAKE_EXIT=2 FAKE_HOST_EXIT=0 sh scripts/lint-touched.sh pkg/a/x.go

tree w3
want "a tagged-only package, e2e pass 2: exit 2" 2 "$t/w3" env FAKE_HOST_EXIT=2 sh scripts/lint-touched.sh pkg/tag/e2e_test.go

tree td
want "a file under testdata is ignored, as the go tool ignores it" 0 "$t/td" sh scripts/lint-touched.sh pkg/a/testdata/f.go
skipped "testdata only" td

tree ign
want "a _ or . path element is ignored, as the go tool ignores it" 0 "$t/ign" sh scripts/lint-touched.sh pkg/_x/f.go pkg/.x/f.go
skipped "_x and .x only" ign

tree bare
want "a directory with no Go file left is skipped" 0 "$t/bare" sh scripts/lint-touched.sh emptydir/gone.go
skipped "a directory with no Go file" bare

tree root
touch "$t/root/x.go"
want "a Go file in the repo root gives ./." 0 "$t/root" sh scripts/lint-touched.sh x.go
expect "root package, plain pass" "packages" "$(pkgs root plain)" "./. "

tree sp
want "a directory name with a space is passed intact" 0 "$t/sp" sh scripts/lint-touched.sh "pkg/a b/s.go"
expect "space path, plain pass" "the argument" "$(cat "$t/sp/plain" 2>/dev/null)" "./pkg/a b"
expect "space path, e2e pass" "the argument" "$(sed -n 4p "$t/sp/host" 2>/dev/null)" "./pkg/a b"

[ "$fails" = 0 ] && { echo "lint_touched_test: all cases pass"; exit 0; }
echo "lint_touched_test: $fails case(s) failed"
exit 1
