# syntax=docker/dockerfile:1

# Both build stages run on the build host; Go cross-compiles for the target.
FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run typecheck && npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# .dockerignore keeps any local dist out; only the fresh build is embedded.
COPY --from=web /src/frontend/dist ./frontend/dist
RUN ./scripts/check-embed.sh
RUN go test ./...
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/posts ./cmd/posts \
 && mkdir -p /out/config /out/data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/posts /app/posts
# config/ holds config.yaml and posts.db; data/ holds the converted posts.
COPY --from=build --chown=nonroot:nonroot /out/config /app/config
COPY --from=build --chown=nonroot:nonroot /out/data /app/data
VOLUME ["/app/config", "/app/data"]
EXPOSE 36751
ENTRYPOINT ["/app/posts", "-config", "/app/config/config.yaml"]
