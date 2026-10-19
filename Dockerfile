# syntax=docker/dockerfile:1

# Kit Maker image: one static Go binary on distroless (DESIGN.md §11.1).
# Runs as non-root with any UID (OpenShift restricted-v2) and a read-only
# root filesystem; it writes nothing outside /tmp.
#
#   docker build -t kitmaker .
#   docker buildx build --platform linux/amd64,linux/arm64 -t kitmaker .

ARG GO_VERSION=1.27
ARG NODE_VERSION=24

# --- Frontend: Vue + Vite build, embedded into the binary below ----------
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# --- Backend: cross-compiled, CGO off --------------------------------------
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/kitmaker ./cmd/kitmaker

# --- Runtime ---------------------------------------------------------------
FROM gcr.io/distroless/static:nonroot AS kitmaker
# World-executable so an arbitrary UID (OpenShift) can run it.
COPY --from=build --chmod=0755 /out/kitmaker /usr/local/bin/kitmaker
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/kitmaker"]
CMD ["serve"]
