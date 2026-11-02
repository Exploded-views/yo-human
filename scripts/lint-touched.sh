#!/bin/sh
# lint-touched.sh [file...]: lint.sh on each Go package the given files belong to, once each. The
# package of an argument ending in .go is its directory. Skipped: a directory that no longer exists
# or holds no .go file (its files were deleted), and a directory the go tool ignores (testdata, or
# a path element starting with _ or .). With no Go package left it prints that and exits 0 without
# running lint.sh. The pre-push hook passes the files a push touches, so every touched package is linted in
# full, not only the lines the push changed. Paths with spaces are passed through intact.
# Exit: lint.sh's exit code (0 clean, 1 issues, 2 golangci-lint failed), or 0 when no Go package
# is left.
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
# Split on newlines only, with no globbing, so each package stays one argument.
IFS=$nl
set -f
set -- $list
unset IFS
set +f
exec sh "$(dirname "$0")/lint.sh" "$@"
