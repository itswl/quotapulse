# syntax=docker/dockerfile:1
#
# Three stages: dashboard -> binary -> runtime image.
#
# The runtime image is empty apart from the binary and CA certificates: time
# zones are compiled in, SQLite is pure Go, and the binary is its own health
# check. Nothing to patch, nothing to exec into.
#
# Both build stages run on the builder's own architecture and cross-compile,
# so a multi-arch build needs no emulation:
#   docker buildx build --platform linux/amd64,linux/arm64 -t quotapulse .
#
# Behind a slow link to the default registries:
#   docker build --build-arg GOPROXY=https://goproxy.cn,direct \
#                --build-arg NPM_REGISTRY=https://registry.npmmirror.com .

ARG GO_IMAGE=golang:1.27-alpine
ARG NODE_IMAGE=node:22-alpine

# ---------- dashboard ----------
FROM --platform=$BUILDPLATFORM ${NODE_IMAGE} AS web
ARG NPM_REGISTRY=https://registry.npmjs.org
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm config set registry "${NPM_REGISTRY}" && npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---------- binary ----------
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build
ARG GOPROXY=https://proxy.golang.org,direct
ARG TARGETOS
ARG TARGETARCH
# The release pipeline passes the tag; anything else reports itself as dev.
ARG VERSION=dev
ENV GOPROXY=${GOPROXY} CGO_ENABLED=0
WORKDIR /src

# Dependencies in their own layer: a code change does not download them again.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
COPY web/embed.go web/embed.go
COPY --from=web /web/dist web/dist
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
      -ldflags="-s -w -X quotapulse/internal/buildinfo.Version=${VERSION}" \
      -o /out/quotapulse ./cmd/quotapulse

# scratch has no mkdir: make the data directory here and copy it with its owner.
RUN mkdir -p /out/data

# ---------- runtime ----------
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/quotapulse /quotapulse
COPY --from=build --chown=65532:65532 /out/data /data

ENV QUOTAPULSE_DATA=/data QUOTAPULSE_LISTEN=:8080
# A numeric user: scratch has no /etc/passwd to look a name up in.
USER 65532:65532
VOLUME /data
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=20s --retries=3 \
    CMD ["/quotapulse", "healthcheck"]

ENTRYPOINT ["/quotapulse"]
