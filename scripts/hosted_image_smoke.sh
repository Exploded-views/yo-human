#!/bin/sh
# hosted_image_smoke.sh [IMAGE]: run the built example image (default $HOSTED_IMAGE, else
# yo-human-hosted:local; build it with `make hosted-image`) and check what the entrypoint tests,
# which run in a busybox container as root, cannot: the image's own /bin/sh and busybox links
# resolve in distroless, it runs as uid 65532, and with no TUNNEL_TOKEN it exits 78 and prints
# only the variable's name. No network; nothing is pushed. Needs Docker.
# Exit: 0 all pass, 1 a case failed, 2 Docker is missing.
set -u
img=${1:-${HOSTED_IMAGE:-yo-human-hosted:local}}
docker=${DOCKER:-docker}
command -v "$docker" >/dev/null 2>&1 || { echo "hosted_image_smoke: $docker not found"; exit 2; }
fails=0
fail() { echo "FAIL $1"; fails=$((fails + 1)); }
ok() { echo "ok   $1"; }

out=$("$docker" run --rm --network none "$img" 2>&1)
rc=$?
[ "$rc" = 78 ] && ok "no token exits 78" || fail "no token: want exit 78, got $rc"
[ "$out" = "TUNNEL_TOKEN is required" ] && ok "only the variable's name is printed" || fail "no token: the output is not the one line naming TUNNEL_TOKEN"

uid=$("$docker" run --rm --network none --entrypoint /bin/busybox "$img" id -u 2>&1)
[ "$uid" = 65532 ] && ok "runs as uid 65532" || fail "the image's uid is not 65532"
user=$("$docker" image inspect --format '{{.Config.User}}' "$img" 2>&1)
[ "$user" = 65532:65532 ] && ok "USER is 65532:65532" || fail "the image's USER is not 65532:65532"

[ "$fails" = 0 ] || { echo "hosted_image_smoke: $fails failure(s)"; exit 1; }
echo "hosted_image_smoke: all cases pass"
