# Example image for a self-hosted yo-human-hosted bus. Build it from the repository root:
#   docker build -f deploy/example/Dockerfile .
# Nothing here is pushed or deployed by the repository. Every base image is pinned by digest.
#
# `file` on the Litestream 0.5.17 binary from its release image:
#   litestream: ELF 64-bit LSB executable, x86-64, statically linked, stripped
# litestream: statically linked, so the final base is distroless static, not alpine. Distroless has
# no shell, and entrypoint.sh needs one: the busybox stage supplies a static busybox, linked as sh,
# env, sed and sleep. Those links only name the applets the entrypoint uses: every other applet
# (wget, nc and the rest) is still reachable as `/bin/busybox <name>`. A busybox built with only
# those four is a follow-up (a pinned source build); it is not done here.

FROM golang:1.27.4@sha256:205c7636d74e0ffbe3d007162df7d74f859f5a8c8322dfb9f907977686c126cf AS build
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH GOFLAGS= GOWORK=off \
    go build -trimpath \
      -ldflags "-s -w -X github.com/Exploded-views/yo-human/internal/version.Version=$VERSION -X github.com/Exploded-views/yo-human/internal/version.Commit=$COMMIT" \
      -o /out/yo-human-hosted ./cmd/yo-human-hosted \
    && mkdir -p /out/data

FROM litestream/litestream:0.5.17@sha256:4b02b9859a6b6b4087d8b8944e15f7e984bd7957cba322bbeee38b0e27b9656a AS litestream

FROM cloudflare/cloudflared:2026.11.1@sha256:e613aadbc2d9511785d2e67a86b0c8ac18dd63a3f9af0d06faf04cbfdcb35f2d AS cloudflared

FROM busybox:1.37.0-musl@sha256:f6a95d611e0a8780fabdb2aec43e31275b2720dfefa3e41b16835bfc42454478 AS busybox
RUN mkdir /out && cp /bin/busybox /out/busybox \
    && for applet in sh env sed sleep; do ln -s busybox /out/$applet; done

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=busybox /out/ /bin/
COPY --from=litestream /usr/local/bin/litestream /usr/local/bin/litestream
COPY --from=cloudflared /usr/local/bin/cloudflared /usr/local/bin/cloudflared
COPY --from=build /out/yo-human-hosted /app/yo-human-hosted
# /data is owned by 65532 here, but a platform volume mounted over it is usually root-owned, and the
# server then cannot create its database: the volume must be chowned to 65532 once (setup.md's job).
COPY --from=build --chown=65532:65532 /out/data /data
COPY --chmod=0555 deploy/example/entrypoint.sh /app/entrypoint.sh
COPY deploy/example/litestream.yml /etc/litestream.yml
USER 65532:65532
ENTRYPOINT ["/app/entrypoint.sh"]
