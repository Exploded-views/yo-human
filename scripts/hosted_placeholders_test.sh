#!/bin/sh
# hosted_placeholders_test.sh: tests for scripts/hosted_placeholders.sh on scratch directories under
# a temp dir, one fixture per rule, plus the repo's own deploy/example/.
# Exit: 0 all cases pass, 1 a case failed.
set -u
root=$(cd "$(dirname "$0")/.." && pwd)
script=$root/scripts/hosted_placeholders.sh
t=$(mktemp -d) || exit 1
trap 'rm -rf "$t"' EXIT
fails=0

# check NAME WANT_EXIT DIR: run the script on DIR, compare the exit code, keep output in $t/out.
check() {
  name=$1 want=$2 dir=$3
  sh "$script" "$dir" >"$t/out" 2>&1
  got=$?
  if [ "$got" != "$want" ]; then
    echo "FAIL $name: want exit $want, got $got"; fails=$((fails + 1)); return 1
  fi
  echo "ok   $name (exit $got)"
}

# expect NAME TEXT: assert the last output contains TEXT; refuse NAME TEXT: assert it does not.
expect() {
  if ! grep -q -- "$2" "$t/out"; then echo "FAIL $1: output lacks '$2'"; fails=$((fails + 1)); fi
}
refuse() {
  if grep -q -- "$2" "$t/out"; then echo "FAIL $1: output holds '$2'"; fails=$((fails + 1)); fi
}

# fixture NAME: a clean example directory, printed.
fixture() {
  d=$t/$1
  mkdir -p "$d"
  cat >"$d/fly.toml" <<'EOF'
app = '<app>'
primary_region = "<region>"
[env]
  YH_LISTEN_MCP = '127.0.0.1:8081'
  YH_PUBLIC_URL = 'https://<mcp-host>'
  YH_BARK_URL = 'https://example.com'
EOF
  cat >"$d/litestream.yml" <<'EOF'
dbs:
  - path: /data/bus.db
    replica:
      type: s3
      bucket: <bucket>
      region: <region>
      endpoint: <endpoint>
EOF
  cat >"$d/Dockerfile" <<'EOF'
# a tunnel to http://127.0.0.1:8081 and http://localhost:8080
FROM scratch
EOF
  printf '%s\n' "$d"
}

# bad NAME FILE LINE: a clean fixture with LINE appended to FILE; the script must refuse it and
# name the file and the line.
bad() {
  d=$(fixture "$1")
  printf '%s\n' "$3" >>"$d/$2"
  check "$1" 1 "$d" && { expect "$1" "$2:"; }
}

d=$(fixture clean)
check clean 0 "$d"

# The repo's own example deploy.
check repo-example 0 "$root/deploy/example"

# Rule platform-host: no platform hostname, with or without a placeholder in front of it.
for h in fly.dev workers.dev trycloudflare.com cfargotunnel.com herokuapp.com vercel.app onrender.com \
  railway.app pages.dev netlify.app ngrok.io tigris.dev flycast; do
  bad "host-$h" fly.toml "# reach it at <app>.$h"
done
d=$(fixture host-in-dockerfile)
echo '# see x.fly.dev' >>"$d/Dockerfile"
check host-in-dockerfile 1 "$d" && { expect host-in-dockerfile "Dockerfile:"; expect host-in-dockerfile platform-host; }
bad host-upper fly.toml "# X.FLY.DEV"

# Rule home-path: no home-directory path.
# The macOS home directory has no fixture: the push scrub refuses that path in any added line.
bad home-linux fly.toml "# build in /home/alice/code/x"
bad home-tilde fly.toml "# build in ~/code/x"
d=$(fixture home-in-yaml)
printf '# /home/alice/x\n' >>"$d/litestream.yml"
check home-in-yaml 1 "$d" && { expect home-in-yaml home-path; expect home-in-yaml "litestream.yml:"; }

# Rule placeholder-value: a name-bearing key holds only a <placeholder>.
bad value-app fly.toml "app = 'realname'"
bad value-app-double fly.toml 'app = "realname"'
bad value-region fly.toml "primary_region = 'ord'"
bad value-bucket litestream.yml "      bucket: realbucket"
bad value-bucket-list litestream.yml "  - bucket: realbucket"
bad value-region-yaml litestream.yml "      region: us-east-1"
bad value-endpoint litestream.yml "      endpoint: s3.region.example.net"
bad value-partial fly.toml "app = 'my-<app>'"
bad value-empty fly.toml "app = ''"
d=$(fixture value-rule-name)
echo "app = 'realname'" >>"$d/fly.toml"
check value-rule-name 1 "$d" && expect value-rule-name placeholder-value

# Rule url-host: a URL names only a loopback address, example.com or a placeholder.
bad url-real fly.toml "# docs: https://docs.real-service.net/x"
bad url-http fly.toml "# docs: http://real-service.net"
bad url-in-env fly.toml "  YH_BARK_URL = 'https://bark.real-service.net'"
bad url-sub fly.toml "# docs: https://sub.example.com.evil.net/x"
bad url-userinfo fly.toml "# docs: https://example.com@real-service.net/x"
bad url-in-dockerfile Dockerfile "# fetch https://real-service.net/x.tgz"
d=$(fixture url-rule-name)
echo '# https://real-service.net' >>"$d/fly.toml"
check url-rule-name 1 "$d" && expect url-rule-name url-host

# Every finding is reported, and none echoes the offending value.
d=$(fixture many)
printf '%s\n' "app = 'realname'" '# /home/alice/x' '# https://real-service.net' '# x.fly.dev' >>"$d/fly.toml"
check many-findings 1 "$d" && {
  expect many-findings placeholder-value; expect many-findings home-path
  expect many-findings url-host; expect many-findings platform-host
  refuse many-findings realname; refuse many-findings alice; refuse many-findings real-service
}

# A scan of nothing is not a pass.
mkdir "$t/empty"
check empty-dir 2 "$t/empty"
check missing-dir 2 "$t/does-not-exist"
check not-a-dir 2 "$t/clean/fly.toml"
sh "$script" a b >"$t/out" 2>&1
[ $? = 2 ] && echo "ok   two-args (exit 2)" || { echo "FAIL two-args: want exit 2"; fails=$((fails + 1)); }

[ "$fails" = 0 ] || { echo "hosted_placeholders_test: $fails failure(s)"; exit 1; }
echo "hosted_placeholders_test: all cases pass"
