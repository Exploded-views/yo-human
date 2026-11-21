#!/bin/sh
# hosted_entrypoint_inner.sh: the entrypoint's behaviour tests. It runs inside the pinned busybox
# container that hosted_entrypoint_test.sh starts (the image's own shell and applets), with
# deploy/example/entrypoint.sh mounted at /app/entrypoint.sh. cloudflared, litestream and
# yo-human-hosted are fakes written here, so no network and no tunnel is involved. Fake secrets are
# obviously fake strings. Exit: 0 all cases pass, 1 a case failed.
set -u
EP=/app/entrypoint.sh
TOKEN=TUNNEL-TOKEN-FAKE
GHSEC=GITHUB-SECRET-FAKE
fails=0
fail() { echo "FAIL $1"; fails=$((fails + 1)); }
ok() { echo "ok   $1"; }

mkdir -p /out /fakebin /app /data || exit 1

# The fakes record what they were started with, then wait. A fake writes its own pid and execs
# sleep, so the pid in the file is the process that holds the environment under test.
cat >/fakebin/cloudflared <<'EOF'
#!/bin/sh
echo "$*" >/out/cloudflared.args
echo $$ >/out/cloudflared.pid
exec sleep 600
EOF
cat >/app/yo-human-hosted <<'EOF'
#!/bin/sh
echo "$*" >/out/serve.args
echo $$ >/out/serve.pid
exec sleep 600
EOF
cat >/fakebin/litestream <<'EOF'
#!/bin/sh
echo "$*" >>/out/litestream.args
case "$1" in
restore) exit "${FAKE_RESTORE_RC:-0}" ;;
replicate)
  [ "$2" = -exec ] || exit 64
  echo $$ >/out/litestream.pid
  sh -c "$3" &
  c=$!
  trap 'kill -TERM $c; wait $c; exit 0' TERM
  wait $c
  exit $?
  ;;
esac
exit 64
EOF
chmod +x /fakebin/cloudflared /app/yo-human-hosted /fakebin/litestream

alive() {
  s=$(cat "/proc/$1/stat" 2>/dev/null) || return 1
  case $s in *") Z "*) return 1 ;; esac
  return 0
}

# start [VAR=value ...]: the entrypoint in the background, with only the given variables on top of
# PATH, in a clean output directory.
start() {
  rm -f /out/* /tmp/timedout
  env -i PATH=/fakebin:/bin HOME=/data "$@" sh "$EP" >/out/ep.out 2>&1 &
  ep=$!
}

# waitfor FILE: wait up to 10s for a non-empty file.
waitfor() {
  i=0
  while [ ! -s "$1" ] && [ "$i" -lt 100 ]; do sleep 0.1; i=$((i + 1)); done
  [ -s "$1" ]
}

# finish SECS: wait for the entrypoint to exit, killing it at the limit; sets rc and timed.
finish() {
  ( sleep "$1"; echo 1 >/tmp/timedout; kill -9 "$ep" 2>/dev/null ) &
  wd=$!
  wait "$ep"
  rc=$?
  kill "$wd" 2>/dev/null
  wait "$wd" 2>/dev/null
  if [ -f /tmp/timedout ]; then timed=1; else timed=0; fi
}

# noleak NAME: the entrypoint's own output holds neither fake secret value.
noleak() {
  if grep -q -e "$TOKEN" -e "$GHSEC" /out/ep.out; then fail "$1: a secret value is in the container output"; fi
}

# environ PID: the process's environment, one NAME=value per line.
environ() { tr '\0' '\n' <"/proc/$1/environ"; }

# running NAME [VAR=value ...]: start, and wait for both children to be up.
running() {
  start "$@"
  if ! waitfor /out/cloudflared.pid || ! waitfor /out/serve.pid || ! waitfor /out/litestream.pid; then
    fail "$1: the children did not start"
    return 1
  fi
}

# --- a missing or empty TUNNEL_TOKEN: exit 78, only the name printed, nothing started ---
for variant in missing empty; do
  if [ "$variant" = empty ]; then start TUNNEL_TOKEN= YH_GITHUB_CLIENT_SECRET=$GHSEC; else start YH_GITHUB_CLIENT_SECRET=$GHSEC; fi
  finish 10
  [ "$timed" = 0 ] || fail "token-$variant: the entrypoint hung"
  [ "$rc" = 78 ] && ok "token-$variant exits 78" || fail "token-$variant: want exit 78, got $rc"
  grep -q TUNNEL_TOKEN /out/ep.out || fail "token-$variant: the message does not name TUNNEL_TOKEN"
  [ "$(wc -l </out/ep.out)" -le 2 ] || fail "token-$variant: more than the name was printed"
  noleak "token-$variant"
  [ ! -e /out/litestream.args ] || fail "token-$variant: restore ran before the token check"
done

# --- the happy path: the commands, the environments and a clean stop on SIGTERM ---
if running happy TUNNEL_TOKEN=$TOKEN YH_GITHUB_CLIENT_SECRET=$GHSEC; then
  want='restore -if-db-not-exists -if-replica-exists /data/bus.db'
  [ "$(sed -n 1p /out/litestream.args)" = "$want" ] && ok "restore comes first, with its flags" || fail "happy: litestream's first call is not '$want'"
  want='replicate -exec /app/yo-human-hosted serve'
  [ "$(sed -n 2p /out/litestream.args)" = "$want" ] && ok "replicate runs serve" || fail "happy: litestream's second call is not '$want'"
  want='tunnel --no-autoupdate --metrics 127.0.0.1:20241 run'
  [ "$(cat /out/cloudflared.args)" = "$want" ] && ok "cloudflared, metrics on loopback" || fail "happy: cloudflared's arguments are not '$want'"
  [ "$(cat /out/serve.args)" = serve ] || fail "happy: serve got other arguments"

  environ "$(cat /out/serve.pid)" >/out/serve.env
  if grep -q -e "$TOKEN" -e '^TUNNEL_TOKEN' /out/serve.env; then fail "happy: serve's environment holds the tunnel token"; else ok "serve's environment lacks the token"; fi
  grep -q "^YH_GITHUB_CLIENT_SECRET=$GHSEC\$" /out/serve.env && ok "serve keeps the app's secrets" || fail "happy: serve lost YH_GITHUB_CLIENT_SECRET"

  environ "$(cat /out/cloudflared.pid)" >/out/cf.env
  grep -q "^TUNNEL_TOKEN=$TOKEN\$" /out/cf.env && ok "cloudflared has its token" || fail "happy: cloudflared lacks TUNNEL_TOKEN"
  if grep -q -e "$GHSEC" -e '^YH_' /out/cf.env; then fail "happy: cloudflared's environment holds an app variable"; else ok "cloudflared's environment lacks the app's secrets"; fi
  extra=$(sed 's/=.*//' /out/cf.env | grep -v -x -e PATH -e HOME -e TUNNEL_TOKEN -e SSL_CERT_FILE -e NO_AUTOUPDATE -e PWD -e '')
  [ -z "$extra" ] || fail "happy: cloudflared has unexpected variables: $(echo $extra)"
  if grep -q "$TOKEN" /out/cloudflared.args; then fail "happy: the token is on cloudflared's command line"; fi

  kill -TERM "$ep"
  finish 10
  [ "$timed" = 0 ] || fail "happy: SIGTERM did not stop the entrypoint"
  [ "$rc" = 0 ] && ok "SIGTERM: clean exit" || fail "happy: after SIGTERM want exit 0, got $rc"
  if alive "$(cat /out/cloudflared.pid)" || alive "$(cat /out/serve.pid)"; then fail "happy: a child outlived SIGTERM"; else ok "SIGTERM stops both children"; fi
  noleak happy
fi

# --- killing a child stops the other and exits non-zero ---
if running kill-serve TUNNEL_TOKEN=$TOKEN YH_GITHUB_CLIENT_SECRET=$GHSEC; then
  cf=$(cat /out/cloudflared.pid)
  kill -9 "$(cat /out/serve.pid)"
  finish 10
  [ "$timed" = 0 ] || fail "kill-serve: the entrypoint did not exit"
  [ "$rc" != 0 ] && [ "$timed" = 0 ] && ok "killing serve exits non-zero ($rc)" || fail "kill-serve: want a non-zero exit, got $rc"
  if alive "$cf"; then fail "kill-serve: cloudflared kept running"; else ok "killing serve stops cloudflared"; fi
  noleak kill-serve
fi

if running kill-cloudflared TUNNEL_TOKEN=$TOKEN YH_GITHUB_CLIENT_SECRET=$GHSEC; then
  sv=$(cat /out/serve.pid)
  ls=$(cat /out/litestream.pid)
  kill -9 "$(cat /out/cloudflared.pid)"
  finish 10
  [ "$timed" = 0 ] || fail "kill-cloudflared: the entrypoint did not exit"
  [ "$rc" != 0 ] && [ "$timed" = 0 ] && ok "killing cloudflared exits non-zero ($rc)" || fail "kill-cloudflared: want a non-zero exit, got $rc"
  if alive "$sv" || alive "$ls"; then fail "kill-cloudflared: serve or litestream kept running"; else ok "killing cloudflared stops serve"; fi
  noleak kill-cloudflared
fi

# --- a child that exits on its own with status 0 still ends the container non-zero ---
start TUNNEL_TOKEN=$TOKEN FAKE_RESTORE_RC=0
waitfor /out/serve.pid && waitfor /out/cloudflared.pid
kill -TERM "$(cat /out/cloudflared.pid)"
finish 10
[ "$timed" = 0 ] && [ "$rc" != 0 ] && ok "a cloudflared stop ends the container non-zero ($rc)" || fail "cloudflared-term: want a non-zero exit, got $rc (timed out: $timed)"

# --- a failed restore stops the start: no children ---
start TUNNEL_TOKEN=$TOKEN FAKE_RESTORE_RC=3
finish 10
[ "$timed" = 0 ] && [ "$rc" != 0 ] && ok "a failed restore exits non-zero ($rc)" || fail "restore-fails: want a non-zero exit, got $rc (timed out: $timed)"
if [ -e /out/serve.pid ] || [ -e /out/cloudflared.pid ]; then fail "restore-fails: a child started after a failed restore"; fi
noleak restore-fails

# --- the script never traces and never prints the environment ---
if grep -v '^ *#' "$EP" | grep -E -q -e 'set +[-+][a-zA-Z]*x' -e 'xtrace' -e 'printenv' -e 'export +-p' -e 'declare +-x'; then
  fail "the entrypoint can trace or print the environment"
else
  ok "no tracing, no environment dump"
fi

[ "$fails" = 0 ] || { echo "hosted_entrypoint_inner: $fails failure(s)"; exit 1; }
echo "hosted_entrypoint_inner: all cases pass"
