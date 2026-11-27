#!/bin/sh
# hosted_placeholders.sh [DIR]: check that the example deploy (default deploy/example/) holds only
# placeholders. Reports FILE:LINE: RULE for each finding and never prints the offending text, so a
# real name that slipped in is not copied into a log. Rules:
#   platform-host      no platform hostname (fly.dev, workers.dev, trycloudflare.com, ...)
#   home-path          no home-directory path: a macOS or Linux home directory, or ~/...
#   placeholder-value  app, primary_region, bucket, region and endpoint hold only a <placeholder>, and
#                      so does every YH_* setting (or one of 127.0.0.1:8081, :8080, /data/bus.db;
#                      YH_DUET_REPLY_PUSH alone may also hold on or off, by name)
#   url-host           a URL names only a loopback address, example.com, example.org or a <placeholder>
# Exit: 0 clean, 1 findings, 2 usage, a missing or empty directory, or a read error.
set -u
[ $# -le 1 ] || { echo "usage: hosted_placeholders.sh [DIR]" >&2; exit 2; }
dir=${1:-$(cd "$(dirname "$0")/.." && pwd)/deploy/example}
[ -d "$dir" ] || { echo "hosted_placeholders: $dir is not a directory" >&2; exit 2; }

files=$(find "$dir" -type f | sort) || exit 2
[ -n "$files" ] || { echo "hosted_placeholders: $dir holds no files" >&2; exit 2; }

prog='function report(rule) { printf "%s:%d: %s\n", name, FNR, rule }
{
  line = $0
  low = tolower(line)
  if (low ~ /(fly\.dev|workers\.dev|trycloudflare\.com|cfargotunnel\.com|herokuapp\.com|vercel\.app|onrender\.com|railway\.app|pages\.dev|netlify\.app|ngrok\.io|tigris\.dev|\.flycast)/) report("platform-host")
  if (line ~ /\/Users\// || line ~ /\/home\/[A-Za-z]/ || line ~ /~\//) report("home-path")
  if (conf && match(line, "^[ \t-]*(app|primary_region|bucket|region|endpoint)[ \t]*[:=]")) {
    v = substr(line, RSTART + RLENGTH)
    sub(/[ \t]+#.*$/, "", v)
    sub(/^[ \t]+/, "", v)
    sub(/[ \t]+$/, "", v)
    sub(/^["\047]/, "", v)
    sub(/["\047]$/, "", v)
    if (v !~ /^<[a-z][a-z0-9-]*>$/) report("placeholder-value")
  }
  if (conf && match(line, "^[ \t]*YH_[A-Z0-9_]*[ \t]*[:=]")) {
    v = substr(line, RSTART + RLENGTH)
    sub(/[ \t]+#.*$/, "", v)
    sub(/^[ \t]+/, "", v)
    sub(/[ \t]+$/, "", v)
    sub(/^["\047]/, "", v)
    sub(/["\047]$/, "", v)
    key = line
    sub(/^[ \t]+/, "", key)
    sub(/[ \t]*[:=].*$/, "", key)
    r = v
    n = gsub(/<[a-z][a-z0-9-]*>/, "", r)
    ok = (v == "127.0.0.1:8081" || v == ":8080" || v == "/data/bus.db" || (n > 0 && (r == "" || r == "https://")))
    if (key == "YH_DUET_REPLY_PUSH" && (v == "on" || v == "off")) ok = 1
    if (!ok) report("placeholder-value")
  }
  rest = low
  while (match(rest, "[a-z][a-z0-9+.-]*://[^/?# \t\047\"]*")) {
    auth = substr(rest, RSTART, RLENGTH)
    rest = substr(rest, RSTART + RLENGTH)
    sub(/^[^:]*:\/\//, "", auth)
    sub(/:[0-9]*$/, "", auth)
    if (auth !~ /^(127\.0\.0\.1|localhost|example\.com|example\.org|<[a-z][a-z0-9-]*>)$/) report("url-host")
  }
}
'

found=0
for f in $files; do
  rel=${f#"$dir"/}
  case $rel in *.toml | *.yml | *.yaml) conf=1 ;; *) conf=0 ;; esac
  out=$(awk -v name="$rel" -v conf="$conf" "$prog" "$f") || { echo "hosted_placeholders: cannot read $rel" >&2; exit 2; }
  if [ -n "$out" ]; then
    printf '%s\n' "$out"
    found=1
  fi
done

if [ "$found" != 0 ]; then
  echo "hosted_placeholders: the example deploy must hold only placeholders"
  exit 1
fi
echo "hosted_placeholders: ok"
