# Stage 1: Build the Go binary
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

# Copy dependency files and download modules
COPY proxy/go.mod proxy/go.sum ./
RUN go mod download

# Copy source files
COPY proxy/ ./

# Compile statically (CGO-free for minimal Alpine runtime compatibility)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o proxy-server .

# Stage 2: Create a minimal release container
FROM alpine:latest

# Install HTTPS CA certs and timezone data (needed for Scryfall HTTPS endpoints)
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy compiled binary
COPY --from=builder /app/proxy-server /app/proxy-server

# Expose default proxy port
EXPOSE 8000

# Set default env variables (bind to 0.0.0.0 inside the container for external mapping)
ENV HOST=0.0.0.0
ENV PORT=8000

# Directory for mounting persistent SQLite database storage
VOLUME ["/data"]

CMD ["/app/proxy-server", "-port=8000", "-db=/data/scryfall.db"]
