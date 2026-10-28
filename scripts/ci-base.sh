#!/bin/sh
# ci-base.sh: the ratchet half of CI that `make ci` cannot do without a base. It finds the merge
# base of HEAD and BASE (default origin/main), the way the coverage and perf jobs do on a pull
# request, and runs `make floors` with --base-floors and `make perf` with --base-budgets taken
# from that commit. When the base commit has no floors or budgets file, that comparison is
# skipped, as in CI, and the script says so.
# Exit: 0 clean, non-zero when a gate fails or the base cannot be resolved (fails closed).
set -eu
base_ref=${BASE:-origin/main}
if ! base=$(git merge-base HEAD "$base_ref"); then
  echo "ci-base: cannot find the merge base of HEAD and $base_ref; fetch it or pass BASE=<commit>" >&2
  exit 2
fi
d=$(mktemp -d)
trap 'rm -rf "$d"' EXIT
floors_args=""
budgets_args=""
if git cat-file -e "$base:coverage-floors.json" 2>/dev/null; then
  git show "$base:coverage-floors.json" > "$d/base-floors.json"
  floors_args="FLOORS_ARGS=--base-floors=$d/base-floors.json"
fi
if git cat-file -e "$base:perf-budgets.json" 2>/dev/null; then
  git show "$base:perf-budgets.json" > "$d/base-budgets.json"
  budgets_args="PERF_ARGS=--base-budgets=$d/base-budgets.json"
fi
floors_note="skipped, no file at base"
budgets_note="skipped, no file at base"
[ -n "$floors_args" ] && floors_note=compared
[ -n "$budgets_args" ] && budgets_note=compared
echo "ci-base: base=$base ($base_ref) floors: $floors_note; budgets: $budgets_note"
make floors $floors_args
make perf $budgets_args
