#!/bin/sh
# hosted_deploy_check_test.sh: tests for scripts/hosted_deploy_check.sh. Each case copies the
# repo's deploy/example/ to a temp dir, breaks one thing with sed, and expects the check to fail
# naming that rule; the unbroken copy and the repo's own directory must pass.
# Exit: 0 all cases pass, 1 a case failed.
set -u
root=$(cd "$(dirname "$0")/.." && pwd)
script=$root/scripts/hosted_deploy_check.sh
src=$root/deploy/example
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

expect() {
  if ! grep -q -- "$2" "$t/out"; then echo "FAIL $1: output lacks '$2'"; fails=$((fails + 1)); fi
}

# copy NAME: a fresh copy of the example directory, printed.
copy() {
  d=$t/$1
  mkdir -p "$d"
  for f in Dockerfile fly.toml litestream.yml entrypoint.sh Dockerfile.dockerignore; do
    [ -f "$src/$f" ] && cp "$src/$f" "$d/$f"
  done
  printf '%s\n' "$d"
}

# mutant NAME RULE FILE SED...: copy, apply the sed expression to FILE, and expect exit 1 naming RULE.
mutant() {
  name=$1 rule=$2 file=$3
  shift 3
  d=$(copy "$name")
  [ -f "$d/$file" ] || { echo "FAIL $name: $file is missing from the example"; fails=$((fails + 1)); return; }
  sed "$@" "$d/$file" >"$d/$file.new" && mv "$d/$file.new" "$d/$file"
  if cmp -s "$d/$file" "$src/$file"; then
    echo "FAIL $name: the mutation changed nothing"; fails=$((fails + 1)); return
  fi
  check "$name" 1 "$d" && expect "$name" "$rule"
}

# appendm NAME RULE FILE TEXT: copy, append TEXT to FILE, and expect exit 1 naming RULE.
appendm() {
  d=$(copy "$1")
  printf '%s\n' "$4" >>"$d/$3"
  check "$1" 1 "$d" && expect "$1" "$2"
}

d=$(copy pristine)
check pristine 0 "$d"
check repo-example 0 "$src"

# Dockerfile
mutant digest-golang digest Dockerfile 's|^\(FROM golang:[^ @]*\)@sha256:[0-9a-f]*|\1|'
mutant digest-litestream digest Dockerfile 's|^\(FROM litestream/litestream:[^ @]*\)@sha256:[0-9a-f]*|\1|'
mutant digest-cloudflared digest Dockerfile 's|^\(FROM cloudflare/cloudflared:[^ @]*\)@sha256:[0-9a-f]*|\1|'
mutant digest-final digest Dockerfile 's|^\(FROM gcr.io/distroless/[^ @]*\)@sha256:[0-9a-f]*|\1|'
mutant digest-short digest Dockerfile 's|^\(FROM golang:[^ ]*@sha256:[0-9a-f]\{20\}\)[0-9a-f]*|\1|'
mutant go-old go-version Dockerfile 's|^FROM golang:1\.27\.[0-9]*|FROM golang:1.27.3|'
mutant go-minor go-version Dockerfile 's|^FROM golang:1\.27\.[0-9]*|FROM golang:1.26.9|'
mutant litestream-old litestream-version Dockerfile 's|litestream/litestream:0\.5\.17|litestream/litestream:0.5.16|'
mutant litestream-latest litestream-version Dockerfile 's|litestream/litestream:0\.5\.17|litestream/litestream:latest|'
mutant no-cloudflared cloudflared Dockerfile 's|cloudflare/cloudflared|cloudflare/other|'
mutant user-root user Dockerfile 's|^USER .*|USER root|'
mutant user-zero user Dockerfile 's|^USER .*|USER 0:0|'
mutant user-named-root user Dockerfile 's|^USER .*|USER 65532:0|'
mutant user-missing user Dockerfile '/^USER /d'
mutant final-alpine final-base Dockerfile 's|gcr.io/distroless/static-debian12|alpine|'
mutant no-file-note file-note Dockerfile '/statically linked/d'
mutant entrypoint-line entrypoint Dockerfile 's|^ENTRYPOINT .*|ENTRYPOINT ["/app/yo-human-hosted"]|'

# fly.toml
mutant kill-timeout kill_timeout fly.toml 's|^\( *\)kill_timeout = .*|\1kill_timeout = 5|'
mutant kill-timeout-missing kill_timeout fly.toml '/^ *kill_timeout/d'
mutant auto-stop auto_stop fly.toml 's|auto_stop_machines = .*|auto_stop_machines = "stop"|'
mutant min-machines min_machines fly.toml 's|min_machines_running = .*|min_machines_running = 0|'
mutant port-8081 internal-port fly.toml 's|internal_port = 8080|internal_port = 8081|'
appendm port-extra internal-port fly.toml '[[services]]
  internal_port = 8081'
mutant mount mount fly.toml 's|destination = .*|destination = "/var/lib"|'
mutant healthz healthz fly.toml 's|/healthz|/health|'
mutant vm-size vm fly.toml 's|shared-cpu-1x|shared-cpu-2x|'
mutant vm-memory vm fly.toml 's|memory = .*|memory = "512mb"|'
mutant app-name placeholder fly.toml "s|^app = .*|app = 'realname'|"
appendm secret-in-env no-secret fly.toml 'YH_BARK_PASSWORD = "x"'
mutant mcp-listen listen fly.toml "s|YH_LISTEN_MCP = .*|YH_LISTEN_MCP = '0.0.0.0:8081'|"
mutant approve-listen listen fly.toml "s|YH_LISTEN_APPROVE = .*|YH_LISTEN_APPROVE = ':8081'|"

# litestream.yml
mutant litestream-db replica-path litestream.yml 's|/data/bus.db|/data/other.db|'
mutant litestream-bucket placeholder litestream.yml 's|bucket: .*|bucket: realbucket|'

# The directory must hold every file.
for f in Dockerfile fly.toml litestream.yml entrypoint.sh; do
  d=$(copy "missing-$f")
  rm -f "$d/$f"
  check "missing-$f" 1 "$d" && expect "missing-$f" "$f"
done

check missing-dir 2 "$t/does-not-exist"
sh "$script" a b >"$t/out" 2>&1
[ $? = 2 ] && echo "ok   two-args (exit 2)" || { echo "FAIL two-args: want exit 2"; fails=$((fails + 1)); }

[ "$fails" = 0 ] || { echo "hosted_deploy_check_test: $fails failure(s)"; exit 1; }
echo "hosted_deploy_check_test: all cases pass"
