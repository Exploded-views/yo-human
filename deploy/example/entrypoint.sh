#!/bin/sh
# The image's entrypoint: restore the database if it is missing, then supervise two children, the
# tunnel connector (cloudflared) and Litestream, which runs the server. When either exits, the
# other is stopped and this script exits non-zero.
#
# Each process gets its own environment: the tunnel token is not in the server's or Litestream's
# restore environment, and cloudflared gets neither the app's secrets nor the replica's keys.
# Nothing here separates them by uid: the three processes run as one user, so a compromised server
# could still read /proc/<pid>/environ of its siblings (a follow-up, which needs a root start).
# The names of secrets are never printed here, and neither are their values or the environment.
# POSIX sh and busybox applets only (env, sed, sleep): this runs in a distroless image with busybox.

# cloudflared binds its metrics server to every interface by default; pin it to loopback. The
# address lives in a lowercase name that no tool reads: cloudflared reads TUNNEL_METRICS itself, and
# an inherited one must not reach it (see keep_env).
cf_metrics=127.0.0.1:20241

if [ -z "${TUNNEL_TOKEN:-}" ]; then
  echo "TUNNEL_TOKEN is required" >&2
  exit 78
fi

# keep_env PATTERN...: in this shell, unset every variable whose name matches none of the patterns.
# Callers run it in a subshell.
keep_env() {
  for name in $(env | sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p'); do
    keep=0
    for pat in "$@"; do
      case $name in
      $pat) keep=1 ;;
      esac
    done
    [ "$keep" = 1 ] || unset "$name" 2>/dev/null
  done
}

# A replica that does not exist yet (the first start) or a database that does (a restart) is not an
# error; any other failure stops the start, so a server never opens an empty database over a
# replica it could not read. Restore gets what Litestream needs (its replica credentials), and not
# the tunnel token or the app's secrets.
(
  keep_env PATH HOME PWD SHLVL SSL_CERT_FILE 'LITESTREAM_*' 'AWS_*'
  exec litestream restore -if-db-not-exists -if-replica-exists /data/bus.db
) || exit 1

# cloudflared's environment: the token and what the binary needs, nothing else.
run_cloudflared() {
  # the address is read before the filter, so an inherited cf_metrics cannot be unset out from under it
  set -- "$cf_metrics"
  keep_env PATH HOME PWD SHLVL SSL_CERT_FILE NO_AUTOUPDATE TUNNEL_TOKEN
  exec cloudflared tunnel --no-autoupdate --metrics "$1" run
}

# The server's environment: everything but the tunnel token.
run_serve() {
  unset TUNNEL_TOKEN
  exec litestream replicate -exec "/app/yo-human-hosted serve"
}

# busybox ash has no dependable `wait -n`, so poll: a child that has exited but not been waited for
# is a zombie, and counts as gone.
alive() {
  { read -r stat <"/proc/$1/stat"; } 2>/dev/null || return 1
  case $stat in *") Z "*) return 1 ;; esac
  return 0
}

# wait_gone SECONDS PID...: wait until every PID is gone, then KILL what is left.
wait_gone() {
  n=$1
  shift
  while [ "$n" -gt 0 ]; do
    any=0
    for p in "$@"; do
      if alive "$p"; then any=1; fi
    done
    [ "$any" = 0 ] && return 0
    sleep 1
    n=$((n - 1))
  done
  for p in "$@"; do
    if alive "$p"; then kill -KILL "$p" 2>/dev/null; fi
  done
}

# The stop budget. fly.toml's kill_timeout is 15 s, after which the platform KILLs the machine.
# The server's own drain (Grace) is 10 s. Worst case, counted from the platform's SIGTERM: up to 1 s
# until the trap runs (after the current sleep), serve's 10 s drain, then up to 2 s more for
# Litestream's last sync: Litestream is KILLed 12 s after it was signalled. cloudflared has nothing
# in flight once the server is gone, so it gets 1 s. About 14 s in all, inside the 15 s with about a
# second of slack for forks and polling. When one child dies on its own (stop_children) both are
# signalled together and whatever still runs 13 s later is KILLed. Without a bound a wedged child
# (cloudflared's own grace is 30 s) would keep a half-dead machine up.
stop_children() {
  kill -TERM "$cf_pid" "$ls_pid" 2>/dev/null
  wait_gone 13 "$cf_pid" "$ls_pid"
  wait
}

# A stop from the platform: pass it on, let Litestream finish its last sync (up to 12 s, then KILL),
# then stop cloudflared (up to 1 s). Report how the server's stop went. A second signal during this
# is ignored: handled, it would restart the bounds above and run past the 15 s.
terminate() {
  trap '' TERM INT
  kill -TERM "$ls_pid" 2>/dev/null
  wait_gone 12 "$ls_pid"
  wait "$ls_pid"
  status=$?
  kill -TERM "$cf_pid" 2>/dev/null
  wait_gone 1 "$cf_pid"
  wait
  exit "$status"
}

(run_cloudflared) &
cf_pid=$!
(run_serve) &
ls_pid=$!
# The trap is set after both forks: a TERM during the restore above ends the shell outright.
trap terminate TERM INT

while alive "$cf_pid" && alive "$ls_pid"; do
  sleep 1
done
trap - TERM INT
if alive "$cf_pid"; then
  wait "$ls_pid"
else
  wait "$cf_pid"
fi
status=$?
stop_children
# Whichever child ended, the container is no longer doing its job.
[ "$status" -ne 0 ] || status=1
exit "$status"
