#!/bin/sh
# grep_status_test.sh: the scripts that count matches tell "nothing matched" (grep exit 1) from
# "grep failed" (exit 2, an unreadable file): a failure must fail the check, never pass as zero.
# Exit: 0 all cases pass, 1 a case failed.
set -u
root=$(cd "$(dirname "$0")/.." && pwd)
t=$(mktemp -d) || exit 1
trap 'chmod -R u+rwx "$t" 2>/dev/null; rm -rf "$t"' EXIT
fails=0

# want NAME CODE CMD...: runs CMD, and fails the case unless it exits CODE.
want() {
  name=$1 code=$2; shift 2
  "$@" >"$t/out" 2>&1
  got=$?
  if [ "$got" != "$code" ]; then
    echo "FAIL $name: exit $got, want $code"; sed 's/^/    /' "$t/out"; fails=$((fails + 1))
  else
    echo "ok   $name"
  fi
}

# A grep that fails as an unreadable file makes it fail (exit 2).
mkdir "$t/stub"
printf '#!/bin/sh\necho "grep: cannot read" >&2\nexit 2\n' >"$t/stub/grep"
chmod +x "$t/stub/grep"

printf 'YO_HUMAN_CONFIG\n' >"$t/clean.bin"
printf 'YO_HUMAN_CONFIG\nYO_HUMAN_TEST_NOW\n' >"$t/hooked.bin"
want "nohooks: a clean binary" 0 sh "$root/scripts/nohooks.sh" "$t/clean.bin"
want "nohooks: a hook name" 1 sh "$root/scripts/nohooks.sh" "$t/hooked.bin"
want "nohooks: grep fails" 2 env PATH="$t/stub:$PATH" sh "$root/scripts/nohooks.sh" "$t/clean.bin"

mkdir "$t/evals"
printf 'nothing to see\n' >"$t/evals/a.txt"
want "leakcheck: clean output" 0 bash "$root/evals/leakcheck.sh" "$t/evals"
# built at runtime so the public-repo scrub stays uniform
printf 'a path /Us''ers/someone/x\n' >"$t/evals/b.txt"
want "leakcheck: a leak" 1 bash "$root/evals/leakcheck.sh" "$t/evals"
rm "$t/evals/b.txt"
if [ "$(id -u)" != 0 ]; then
  printf 'unreadable\n' >"$t/evals/c.txt" && chmod 000 "$t/evals/c.txt"
  want "leakcheck: an unreadable output" 2 bash "$root/evals/leakcheck.sh" "$t/evals"
fi

[ "$fails" = 0 ] && { echo "grep_status_test: all cases pass"; exit 0; }
echo "grep_status_test: $fails case(s) failed"; exit 1
