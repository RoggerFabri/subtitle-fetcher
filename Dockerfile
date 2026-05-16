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

# tzdata for timezones, ca-certificates for HTTPS, sqlite for any potential DB tools (optional but good to have)
RUN apk --no-cache add ca-certificates tzdata sqlite

WORKDIR /app
COPY --from=builder /src/subtitle-fetcher /app/subtitle-fetcher
COPY --from=builder /src/src/web /app/web

# Create data directory for database
RUN mkdir -p /app/data

# Environment variables
ENV PORT=8080
ENV DB_PATH=/app/data

# Expose web server port
EXPOSE $PORT

# Define volumes for database and media
VOLUME ["/app/data", "/media"]

ENTRYPOINT ["/app/subtitle-fetcher"]

# By default, serve the /media volume
CMD ["--serve", "/media"]
