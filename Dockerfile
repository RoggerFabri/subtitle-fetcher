FROM golang:alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
# Build statically linked binary
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o subtitle-fetcher ./src

FROM alpine:latest

# tzdata for timezones, ca-certificates for HTTPS, shadow+su-exec for PUID/PGID drop.
# The web UI is embedded into the binary via go:embed, so no static assets are copied.
RUN apk --no-cache add ca-certificates tzdata shadow su-exec

WORKDIR /app
COPY --from=builder /src/subtitle-fetcher /app/subtitle-fetcher
COPY entrypoint.sh /entrypoint.sh

# Create data directory and the runtime user remapped at startup by entrypoint.sh
RUN mkdir -p /app/data \
    && addgroup -S appgroup \
    && adduser -S -G appgroup appuser \
    && chmod +x /entrypoint.sh

# Environment variables
ENV PORT=8080
ENV DB_PATH=/app/data
# Default to uid/gid 1000; override via PUID/PGID env (see docker-compose.yaml).
ENV PUID=1000
ENV PGID=1000

# Expose web server port
EXPOSE $PORT

# Define volumes for database and media
VOLUME ["/app/data", "/media"]

# entrypoint.sh remaps appuser to PUID/PGID, applies TZ, then drops privileges.
ENTRYPOINT ["/entrypoint.sh"]

# By default, serve the /media volume
CMD ["--serve", "/media"]
