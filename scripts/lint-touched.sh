#!/bin/sh
# lint-touched.sh [file...]: lint the Go packages the given files belong to, once each, as make lint
# does. The package of an argument ending in .go is its directory. Skipped: a directory that no
# longer exists or holds no .go file (its files were deleted), and a directory the go tool ignores
# (testdata, or a path element starting with _ or .). A package with no file under the default
# build tags is linted only by the e2e pass; the others by lint.sh and by that pass too. The e2e
# pass is lint.sh --host-only with -tags e2e, so every package is linted with the e2e tag. The
# pre-push hook passes the files a push touches, so every touched package is linted in full, not
# only the lines the push changed. Paths with spaces are passed through intact. With no Go package
# left it prints that and exits 0 without running lint.sh.
# Exit: the worst code of the passes (2 golangci-lint failed, 1 issues, 0 clean); 0 when no Go
# package is left.
set -u
nl='
'
list=$nl # each package is a line, framed by newlines: "\n./a\n./b\n"
for f in "$@"; do
  case $f in *.go) ;; *) continue ;; esac
  d=$(dirname -- "$f")
  [ -d "$d" ] || continue
  case /$d/ in */testdata/* | */[._][!/]*) continue ;; esac
  # golangci-lint fails on a directory with no Go file, so one with none left is skipped.
  has=
  for g in "$d"/*.go; do
    if [ -e "$g" ]; then has=1; break; fi
  done
  [ -n "$has" ] || continue
  case $list in *"$nl./$d$nl"*) continue ;; esac
  list="$list./$d$nl"
done
if [ "$list" = "$nl" ]; then
  echo "lint-touched: no Go packages touched"
  exit 0
fi

# The packages with no file under the default build tags: go list counts zero in all three lists.
plain=$nl
while IFS= read -r p; do
  [ -n "$p" ] || continue
  counts=$(go list -e -f '{{len .GoFiles}} {{len .TestGoFiles}} {{len .XTestGoFiles}}' "$p" 2>/dev/null) || counts=
  if [ "$counts" != "0 0 0" ]; then plain="$plain$p$nl"; fi
done <<PKGS
$list
PKGS

worst=0
note() { # keep the worst code: an error (2) beats issues (1) beats clean (0)
  if [ "$1" -gt "$worst" ]; then worst=$1; fi
}
# Split on newlines only, with no globbing, so each package stays one argument.
if [ "$plain" != "$nl" ]; then
  IFS=$nl
  set -f
  set -- $plain
  unset IFS
  set +f
  sh "$(dirname "$0")/lint.sh" "$@"
  note $?
fi
IFS=$nl
set -f
set -- $list
unset IFS
set +f
sh "$(dirname "$0")/lint.sh" --host-only --build-tags e2e "$@"
note $?
exit "$worst"
