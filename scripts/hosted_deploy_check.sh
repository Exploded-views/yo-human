#!/bin/sh
# hosted_deploy_check.sh [DIR]: check the example deploy (default deploy/example/) against Task 12's
# image and fly.toml bullets. Reports RULE: what is wrong, one line each. Rules: digest,
# go-version, litestream-version, cloudflared, user, final-base, file-note, entrypoint (the
# Dockerfile); kill_timeout, auto_stop, min_machines, internal-port, mount, healthz, vm, listen,
# no-secret, placeholder (fly.toml); replica-path, placeholder (litestream.yml). A missing file
# is reported by name.
# Exit: 0 all hold, 1 a rule failed, 2 usage or a read error.
set -u
[ $# -le 1 ] || { echo "usage: hosted_deploy_check.sh [DIR]" >&2; exit 2; }
dir=${1:-$(cd "$(dirname "$0")/.." && pwd)/deploy/example}
[ -d "$dir" ] || { echo "hosted_deploy_check: $dir is not a directory" >&2; exit 2; }
bad=0
fail() { echo "$1: $2"; bad=1; }

# has FILE ERE: does a line match? grep's failure (status 2) is a read error, not "no".
has() {
  grep -E -q -- "$2" "$1"
  case $? in 0) return 0 ;; 1) return 1 ;; *) echo "hosted_deploy_check: cannot read $1" >&2; exit 2 ;; esac
}
# lines FILE ERE: the matching lines, status 2 on a read error.
lines() {
  grep -E -- "$2" "$1"
  [ $? -le 1 ] || { echo "hosted_deploy_check: cannot read $1" >&2; exit 2; }
}
# need FILE RULE ERE MESSAGE
need() { has "$1" "$3" || fail "$2" "$4"; }

for f in Dockerfile fly.toml litestream.yml entrypoint.sh; do
  [ -f "$dir/$f" ] || fail "$f" "missing"
done

d=$dir/Dockerfile
if [ -f "$d" ]; then
  froms=$(lines "$d" '^FROM ')
  # every FROM is pinned by a full digest
  if [ -n "$(printf '%s\n' "$froms" | grep -E -v '@sha256:[0-9a-f]{64}( |$)')" ]; then
    fail digest "a FROM is not pinned by a full 64-hex digest"
  fi
  patch=$(printf '%s\n' "$froms" | sed -n 's/^FROM golang:1\.27\.\([0-9][0-9]*\)@.*/\1/p' | head -n 1)
  { [ -n "$patch" ] && [ "$patch" -ge 4 ]; } || fail go-version "the build stage is not golang 1.27.4 or a later 1.27 patch"
  need "$d" litestream-version '^FROM litestream/litestream:0\.5\.17@' "Litestream is not 0.5.17 from its release image"
  need "$d" cloudflared '^FROM cloudflare/cloudflared:[0-9]' "cloudflared is not copied from its release image by a versioned tag"
  last_from=$(grep -n '^FROM ' "$d" | tail -n 1)
  case $last_from in *"FROM gcr.io/distroless/static-debian12:"*@sha256:*) ;; *) fail final-base "the final base is not distroless static-debian12 by digest" ;; esac
  last_from_n=${last_from%%:*}
  user=$(grep -n '^USER ' "$d" | tail -n 1)
  user_n=${user%%:*}
  user_v=${user#*:USER }
  if [ -z "$user" ] || [ "$user_n" -lt "$last_from_n" ]; then
    fail user "the final stage sets no USER"
  else
    printf '%s\n' "$user_v" | grep -E -q '^[1-9][0-9]*(:[1-9][0-9]*)?$' || fail user "the final USER is not a numeric non-root uid and gid"
  fi
  # every COPY --from names a stage defined by an earlier FROM ... AS
  stray=$(awk '/^FROM / { for (i = 1; i < NF; i++) if (toupper($i) == "AS") stages[$(i + 1)] = 1 }
    /^COPY / { for (i = 1; i <= NF; i++) if ($i ~ /^--from=/) { n = substr($i, 8); if (!(n in stages)) print NR } }' "$d") || exit 2
  [ -z "$stray" ] || fail digest "a COPY --from does not name an earlier stage (line $(echo $stray))"
  need "$d" file-note '^#.*ELF .*statically linked' "the Dockerfile does not record the file result for the Litestream binary"
  need "$d" entrypoint '^ENTRYPOINT \["/app/entrypoint\.sh"\]$' "ENTRYPOINT is not /app/entrypoint.sh"
fi

f=$dir/fly.toml
if [ -f "$f" ]; then
  need "$f" kill_timeout '^kill_timeout = 15( *#.*)?$' "kill_timeout must be 15"
  need "$f" auto_stop "^ *auto_stop_machines = ['\"]off['\"]" 'auto_stop_machines must be "off"'
  need "$f" min_machines '^ *min_machines_running = 1( *#.*)?$' "min_machines_running must be 1"
  ports=$(lines "$f" '^ *internal_port')
  [ "$(printf '%s\n' "$ports" | grep -c .)" = 1 ] || fail internal-port "exactly one service with one internal port is allowed"
  printf '%s\n' "$ports" | grep -E -q '^ *internal_port = 8080( *#.*)?$' || fail internal-port "the service's internal port must be 8080"
  if [ -n "$(grep -v '^ *#' "$f" | grep 8081 | grep -v YH_LISTEN_MCP)" ]; then
    fail internal-port "8081 (the MCP listener) must not be mapped"
  fi
  need "$f" mount "^ *destination = ['\"]/data['\"]" "the volume must mount at /data"
  need "$f" healthz "^ *path = ['\"]/healthz['\"]" "the check must be GET /healthz"
  need "$f" vm "^ *size = ['\"]shared-cpu-1x['\"]" "the machine must be shared-cpu-1x"
  need "$f" vm "^ *memory = ['\"]256mb['\"]" "the machine must have 256 MB"
  need "$f" listen "^ *YH_LISTEN_MCP = ['\"]127\.0\.0\.1:8081['\"]" "the MCP listener must be 127.0.0.1:8081"
  need "$f" listen "^ *YH_LISTEN_APPROVE = ['\"]:8080['\"]" "the approval listener must be :8080"
  need "$f" placeholder "^app = ['\"]<app>['\"]" "app must be the <app> placeholder"
  need "$f" placeholder "^primary_region = ['\"]<region>['\"]" "primary_region must be the <region> placeholder"
  if [ -n "$(grep -v '^ *#' "$f" | grep -E '^ *[A-Z0-9_]*(SECRET|PASSWORD|KEY|TOKEN)[A-Z0-9_]* *=')" ]; then
    fail no-secret "a credential-named setting is in fly.toml; secrets go in through fly secrets import"
  fi
fi

f=$dir/litestream.yml
if [ -f "$f" ]; then
  need "$f" replica-path '^ *- path: /data/bus\.db$' "the replicated database must be /data/bus.db"
  need "$f" placeholder '^ *bucket: <bucket>$' "bucket must be the <bucket> placeholder"
  if [ -n "$(lines "$f" '^ *(access-key-id|secret-access-key) *:' | grep -v -E ': *\$\{[A-Za-z_]+\} *$')" ]; then
    fail no-secret "a replica credential is in litestream.yml; use an \${ENV} reference or a secret"
  fi
fi

if [ "$bad" != 0 ]; then
  echo "hosted_deploy_check: the example deploy does not meet Task 12's image and fly.toml rules"
  exit 1
fi
echo "hosted_deploy_check: ok"
