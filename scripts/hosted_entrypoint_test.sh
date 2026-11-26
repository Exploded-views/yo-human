#!/bin/sh
# hosted_entrypoint_test.sh: runs deploy/example/entrypoint.sh's tests (hosted_entrypoint_inner.sh)
# in the busybox image that the example Dockerfile pins, so the shell and its applets are the
# image's own. It needs Docker and pulls that image, so it is not part of `make test`: run it with
# `make hosted-image-test`. No network inside the container, nothing pushed.
# Exit: 0 all cases pass, 1 a case failed, 2 Docker or the files are missing.
set -u
root=$(cd "$(dirname "$0")/.." && pwd)
docker=${DOCKER:-docker}
ep=$root/deploy/example/entrypoint.sh
df=$root/deploy/example/Dockerfile
inner=$root/scripts/hosted_entrypoint_inner.sh

[ -f "$df" ] || { echo "FAIL deploy/example/Dockerfile is missing"; exit 1; }
[ -f "$ep" ] || { echo "FAIL deploy/example/entrypoint.sh is missing"; exit 1; }
[ -f "$inner" ] || { echo "FAIL scripts/hosted_entrypoint_inner.sh is missing"; exit 1; }
command -v "$docker" >/dev/null 2>&1 || { echo "hosted_entrypoint_test: $docker not found"; exit 2; }

img=$(sed -n 's/^FROM \(busybox:[^ @]*@sha256:[0-9a-f]\{64\}\) AS .*$/\1/p' "$df")
[ -n "$img" ] || { echo "FAIL the Dockerfile has no digest-pinned busybox stage"; exit 1; }

"$docker" run --rm --network none \
  -v "$ep:/app/entrypoint.sh:ro" -v "$inner:/inner.sh:ro" \
  "$img" sh /inner.sh
