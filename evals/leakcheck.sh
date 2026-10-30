#!/usr/bin/env bash
# leakcheck.sh DIR: fail if any eval output in DIR still holds a ULID, a timestamp, a session id,
# something key-like, a /Users/, /home/ or /var/folders/ (macOS temp, which names the machine) path, an IPv4 address, an email address or a hostname
# under a common TLD. Placeholders allowed: example.com/.org/.net and /home/user. Adapted from
# crewkit's evals/leakcheck.sh. Prints counts and file names only, never the matched text.
set -euo pipefail
dir=$1
pat='\b[0-9A-HJKMNP-TV-Z]{26}\b|[0-9]{4}-[0-9]{2}-[0-9]{2}[T ][0-9]{2}:[0-9]{2}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|\b(sk|pk|ghp|gho|AKIA)[-_A-Za-z0-9]{12,}|/Users/|/var/folders/|session_[0-9A-Za-z]{10,}|\b[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\b|[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+\.[A-Za-z]{2,}'
host='\b([a-z0-9-]+\.)+(com|net|org|io|dev|app|cloud|local|internal|lan|co|us|me)\b'
# g: grep, where nothing matching (exit 1) is not a failure. grep failing (exit 2, an unreadable
# file) still is: with pipefail and set -e it stops the check, so it never passes as zero matches.
g() { grep "$@" || (( $? == 1 )) || exit 2; }
hits=0
for f in "$dir"/*; do
  [[ -f $f ]] || continue
  n=$(g -Ec -- "$pat" "$f")
  t=$(g -oE -- '[A-Za-z0-9+/_-]{32,}' "$f" | g -c '[0-9]')
  h=$(g -oiE -- "$host" "$f" | g -viE '(^|\.)example\.(com|org|net)$' | g -c .)
  p=$(g -oE -- '/home/[A-Za-z0-9._-]+' "$f" | g -vxF '/home/user' | g -c .)
  n=$((n + t + h + p))
  if (( n > 0 )); then echo "  leak  $(basename "$f")  $n line(s)"; hits=$((hits + n)); fi
done
(( hits == 0 )) && { echo "leakcheck: clean"; exit 0; }
echo "leakcheck: $hits line(s) hold ids, timestamps, paths, addresses or key-like text"; exit 1
