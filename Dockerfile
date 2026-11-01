# syntax=docker/dockerfile:1
#
# Three-stage build: frontend → binary → runtime image.
# The runtime image is scratch and contains only the static binary and CA certificates:
#   - time-zone data is compiled in with time/tzdata
#   - SQLite uses the pure-Go modernc.org/sqlite driver, so CGO and libc are unnecessary
#   - the binary's -healthcheck command provides the container health check
#
# Multi-architecture builds: buildx selects the target platform.
#   docker buildx build --platform linux/amd64,linux/arm64 -t <registry>/quotapulse --push .
# Optional mirrors for restricted network environments:
#   docker build --build-arg GOPROXY=https://goproxy.cn,direct \
#                --build-arg NPM_REGISTRY=https://registry.npmmirror.com .

# Keep the Go image version compatible with the go directive in go.mod.
ARG GO_IMAGE=golang:1.27-alpine
ARG NODE_IMAGE=node:22-alpine

# ---------- Frontend ----------
# Build on the builder architecture: the output is platform-independent static files.
FROM --platform=$BUILDPLATFORM ${NODE_IMAGE} AS ui
ARG NPM_REGISTRY=https://registry.npmjs.org
WORKDIR /ui
COPY ui/package.json ui/package-lock.json* ./
RUN npm config set registry "${NPM_REGISTRY}" && npm ci --no-audit --no-fund
COPY ui/ ./
RUN npm run build

# ---------- Binary ----------
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build
ARG GOPROXY=https://proxy.golang.org,direct
ARG TARGETOS
ARG TARGETARCH
# The release pipeline passes the version from the git tag. Without it, the binary reports dev.
ARG VERSION=dev
ENV GOPROXY=${GOPROXY} CGO_ENABLED=0
WORKDIR /src

# Keep dependencies in a separate layer so code changes do not redownload them.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Replace the checked-in frontend with the output from this build.
COPY --from=ui /ui/dist ./ui/dist
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
      -ldflags="-s -w -X github.com/itswl/quotapulse/internal/config.Version=${VERSION}" \
      -o /out/quotapulse ./cmd/quotapulse

# Create writable runtime directories before copying them; scratch has no mkdir.
RUN mkdir -p /out/data /out/logs

# ---------- Runtime image ----------
FROM scratch
ENV TZ=Asia/Shanghai
WORKDIR /app

# Provider HTTPS requests need root certificates.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/quotapulse /app/quotapulse
COPY --from=build --chown=65532:65532 /out/data /app/data
COPY --from=build --chown=65532:65532 /out/logs /app/logs

# Run as a non-root numeric UID; scratch has no /etc/passwd.
USER 65532:65532

HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
    CMD ["/app/quotapulse", "-healthcheck"]

EXPOSE 8080 9100
ENTRYPOINT ["/app/quotapulse"]
