#!/bin/sh
# secrets.sh: scan the whole git history with gitleaks. This script is the 0/1/2 contract: CI
# and the tests call it directly. `make secrets` is a convenience and turns exit 1 into make's 2.
# Pass 1 uses the committed .gitleaks.toml. When GITLEAKS_EXTRA names a readable file, pass 2
# uses that file as its config. Output: the number of findings per pass and, per finding, the
# rule id, file and short commit. Never the match or the secret, not even redacted: gitleaks'
# own log goes to a file in a temp dir that is deleted, and only the summary is printed.
# Exit: 0 clean, 1 findings, 2 scanner or report error (fails closed). It also exits 2 unless the
# scan provably covered history: HEAD must resolve, the clone must not be shallow,
# `git fsck --connectivity-only --no-dangling` must pass (a missing object at any depth fails the
# scan), gitleaks' log must hold no ERR or FTL line, and when the history has any commit gitleaks
# must have scanned at least one. The scanned count is not compared to the commit count:
# gitleaks counts only commits with added lines, so delete-only, binary, rename, mode-only and
# empty commits legitimately make the two differ. Every git command's status is checked and its
# output goes to a file before it is parsed, so a pipeline never hides a git failure.
# With --staged (the pre-commit hook) it scans only the staged diff, with the same config,
# allow-comment and ignore-file safeguards; there may be no HEAD yet (a first commit), and the
# history checks do not apply. Any other argument is a usage error (exit 2).
set -u
mode=history
case "${1-}" in
  "") ;;
  --staged) mode=staged ;;
  *) echo "usage: scripts/secrets.sh [--staged]" >&2; exit 2 ;;
esac
d=$(mktemp -d) || exit 2
trap 'rm -rf "$d"' EXIT
mkdir "$d/noignore" || exit 2
trap 'exit 2' INT TERM

# summarise LABEL REPORT: print the summary of a gitleaks JSON report; exit 1 on findings, 2 on a
# report that is not a complete JSON array.
summarise() {
  awk -v label="$1" '
    { buf = buf $0 "\n" }
    END {
      gsub(/^[ \t\r\n]+/, "", buf); gsub(/[ \t\r\n]+$/, "", buf)
      if (buf == "null") buf = "[]"
      if (substr(buf, 1, 1) != "[" || substr(buf, length(buf), 1) != "]") exit 2
      n = 0
      while (match(buf, /"(RuleID|File|Commit)"[ \t]*:[ \t]*"([^"\\]|\\.)*"/)) {
        tok = substr(buf, RSTART, RLENGTH); buf = substr(buf, RSTART + RLENGTH)
        key = tok; sub(/^"/, "", key); sub(/".*/, "", key)
        val = tok; sub(/^"[A-Za-z]+"[ \t]*:[ \t]*"/, "", val); sub(/"$/, "", val)
        if (key == "RuleID") { n++; rule[n] = val; file[n] = "-"; commit[n] = "-" }
        else if (n > 0 && key == "File") file[n] = val
        else if (n > 0) commit[n] = substr(val, 1, 12)
      }
      printf "secrets%s: %d findings\n", label, n
      for (i = 1; i <= n; i++) printf "  %s %s %s\n", rule[i], file[i], commit[i]
      exit (n > 0 ? 1 : 0)
    }' "$2"
}

# pass NAME LABEL CONFIG: one gitleaks run over every ref; returns 0, 1 (findings) or 2 (error).
pass() {
  if [ ! -r "$3" ]; then echo "secrets$2: config is not readable" >&2; return 2; fi
  rep=$d/$1.json
  # --exit-code 0: a finding is not an error, so any non-zero exit is a scanner error.
  # --diff-merges=first-parent: a merge commit is scanned against its first parent, so a key that
  # only a merge adds (an evil merge) is seen. --ignore-gitleaks-allow: an inline gitleaks:allow
  # comment never hides a finding. A .gitleaksignore never hides one either: gitleaks reads it
  # from --gitleaks-ignore-path (an empty dir of ours) and from its source path, so the source is
  # the git dir, which no commit can write to, not the work tree. Fake values are allowlisted in
  # .gitleaks.toml only.
  if ! gitleaks git --redact --no-banner --no-color --log-opts="--all --diff-merges=first-parent" \
      --ignore-gitleaks-allow --gitleaks-ignore-path "$d/noignore" --config "$3" \
      --report-format json --report-path "$rep" --exit-code 0 "$gitdir" >"$d/$1.log" 2>&1; then
    echo "secrets$2: scanner error" >&2; return 2
  fi
  if [ ! -s "$rep" ]; then echo "secrets$2: no report written" >&2; return 2; fi
  # Fail closed: gitleaks exits 0 and writes [] when git itself fails. Read its log with grep
  # only; the log is never printed.
  if grep -Eq '(^|[^A-Za-z])(ERR|FTL)([^A-Za-z]|$)' "$d/$1.log"; then
    echo "secrets$2: scanner logged an error" >&2; return 2
  fi
  scanned=$(sed -n 's/.*[^0-9]\([0-9][0-9]*\) commits scanned.*/\1/p' "$d/$1.log" | head -n 1)
  if [ -z "$scanned" ] || { [ "$total" -gt 0 ] && [ "$scanned" -eq 0 ]; }; then
    echo "secrets$2: gitleaks scanned no commits of a non-empty history" >&2; return 2
  fi
  summarise "$2" "$rep"
  rc=$?
  if [ "$rc" = 2 ]; then echo "secrets$2: report is not a JSON array" >&2; fi
  return "$rc"
}

# staged: one gitleaks run over the staged diff (index against HEAD). gitleaks reads a
# .gitleaksignore from its source dir, so the source is an empty dir of ours whose .git file
# points at this repo's git dir: git diff --staged needs only the index and HEAD, never the work
# tree, and nothing in the work tree can suppress a finding.
staged() {
  if ! gitdir=$(git rev-parse --absolute-git-dir 2>/dev/null) || [ -z "$gitdir" ] ||
      [ "$(git rev-parse --is-inside-work-tree 2>/dev/null)" != true ]; then
    echo "secrets (staged): not inside a git work tree" >&2; return 2
  fi
  if [ -e "$gitdir/.gitleaksignore" ]; then
    echo "secrets (staged): $gitdir/.gitleaksignore exists and would suppress findings" >&2; return 2
  fi
  # Inside a hook git may set GIT_DIR, GIT_WORK_TREE and GIT_INDEX_FILE, possibly relative to
  # the repo root; gitleaks runs git from $d/src, so drop the first two (the .git file says where
  # the git dir is) and make the index path absolute.
  unset GIT_DIR GIT_WORK_TREE
  case "${GIT_INDEX_FILE-}" in
    "" | /*) ;;
    *) GIT_INDEX_FILE=$(pwd)/$GIT_INDEX_FILE; export GIT_INDEX_FILE ;;
  esac
  mkdir "$d/src" && printf 'gitdir: %s\n' "$gitdir" >"$d/src/.git" || return 2
  rep=$d/staged.json
  if ! gitleaks git --staged --redact --no-banner --no-color --ignore-gitleaks-allow \
      --gitleaks-ignore-path "$d/noignore" --config "$here/.gitleaks.toml" \
      --report-format json --report-path "$rep" --exit-code 0 "$d/src" >"$d/staged.log" 2>&1; then
    echo "secrets (staged): scanner error" >&2; return 2
  fi
  if [ ! -s "$rep" ]; then echo "secrets (staged): no report written" >&2; return 2; fi
  if grep -Eq '(^|[^A-Za-z])(ERR|FTL)([^A-Za-z]|$)' "$d/staged.log"; then
    echo "secrets (staged): scanner logged an error" >&2; return 2
  fi
  summarise " (staged)" "$rep"
  rc=$?
  if [ "$rc" = 2 ]; then echo "secrets (staged): report is not a JSON array" >&2; fi
  return "$rc"
}

here=$(cd "$(dirname "$0")/.." && pwd)
if [ "$mode" = staged ]; then
  staged
  exit $?
fi
if ! git rev-parse --verify -q HEAD >/dev/null 2>&1; then
  echo "secrets: not a git repository with a HEAD commit" >&2; exit 2
fi
if ! gitdir=$(git rev-parse --absolute-git-dir 2>/dev/null) || [ -z "$gitdir" ]; then
  echo "secrets: cannot resolve the git dir" >&2; exit 2
fi
if [ -e "$gitdir/.gitleaksignore" ]; then
  echo "secrets: $gitdir/.gitleaksignore exists and would suppress findings" >&2; exit 2
fi
if [ "$(git rev-parse --is-shallow-repository 2>/dev/null)" != false ]; then
  echo "secrets: shallow clone, history is incomplete (use fetch-depth: 0)" >&2; exit 2
fi
if ! git fsck --connectivity-only --no-dangling >"$d/fsck.log" 2>&1; then
  echo "secrets: git fsck failed, the object database is incomplete or corrupt" >&2; exit 2
fi
if ! git rev-list --all --count >"$d/count.txt" 2>"$d/count.err"; then
  echo "secrets: git rev-list failed" >&2; exit 2
fi
total=$(cat "$d/count.txt")
case $total in '' | *[!0-9]*) echo "secrets: bad commit count" >&2; exit 2 ;; esac
worst=0
note() { # keep the worst code: error 2 beats findings 1 beats clean 0
  [ "$1" -gt "$worst" ] && worst=$1
  return 0
}
pass repo "" "$here/.gitleaks.toml"; note $?
if [ -n "${GITLEAKS_EXTRA:-}" ]; then
  pass extra " (extra)" "$GITLEAKS_EXTRA"; note $?
fi
exit "$worst"
