#!/bin/sh
# The image's entrypoint: restore the database if it is missing, then supervise two children, the
# tunnel connector (cloudflared) and Litestream, which runs the server. When either exits, the
# other is stopped and this script exits non-zero.
#
# The tunnel token is the only secret cloudflared gets, and the server never sees it. The names of
# secrets are never printed here, and neither are their values or the environment.
# POSIX sh and busybox applets only (env, sed, sleep): this runs in a distroless image with busybox.

TUNNEL_METRICS=127.0.0.1:20241 # cloudflared binds every interface by default; pin it to loopback

if [ -z "${TUNNEL_TOKEN:-}" ]; then
  echo "TUNNEL_TOKEN is required" >&2
  exit 78
fi

# A replica that does not exist yet (the first start) or a database that does (a restart) is not an
# error; any other failure stops the start, so a server never opens an empty database over a
# replica it could not read.
litestream restore -if-db-not-exists -if-replica-exists /data/bus.db || exit 1

# cloudflared's environment: the token and what the binary needs, nothing else. The app's secrets
# (YH_*, the replica's keys) are in this shell's environment and are dropped here, in a subshell.
run_cloudflared() {
  for name in $(env | sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p'); do
    case $name in
    PATH | HOME | PWD | SSL_CERT_FILE | NO_AUTOUPDATE | TUNNEL_TOKEN) ;;
    *) unset "$name" 2>/dev/null ;;
    esac
  done
  exec cloudflared tunnel --no-autoupdate --metrics "$TUNNEL_METRICS" run
}

# The server's environment: everything but the tunnel token.
run_serve() {
  unset TUNNEL_TOKEN
  exec litestream replicate -exec "/app/yo-human-hosted serve"
}

stop_children() {
  kill -TERM "$cf_pid" "$ls_pid" 2>/dev/null
  wait
}

# A stop from the platform: pass it on, let Litestream finish its last sync, and report how it went.
terminate() {
  kill -TERM "$ls_pid" 2>/dev/null
  wait "$ls_pid"
  status=$?
  kill -TERM "$cf_pid" 2>/dev/null
  wait
  exit "$status"
}

(run_cloudflared) &
cf_pid=$!
(run_serve) &
ls_pid=$!
trap terminate TERM INT

# busybox ash has no dependable `wait -n`, so poll: a child that has exited but not been waited for
# is a zombie, and counts as gone.
alive() {
  { read -r stat <"/proc/$1/stat"; } 2>/dev/null || return 1
  case $stat in *") Z "*) return 1 ;; esac
  return 0
}
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
