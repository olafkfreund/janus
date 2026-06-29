# --- Stage 1: Build the Go backend binary ---
# Use a specific, pinned Go Alpine image for build reproducibility and security
FROM golang:1.26-alpine AS builder

# Install build dependencies for static CGO (required by go-sqlite3)
RUN apk add --no-cache gcc musl-dev sqlite-dev sqlite-static

WORKDIR /app

# Copy dependency files
COPY go.mod ./
COPY go.sum* ./

# Download dependencies (highly cached in Docker layer)
RUN go mod download

# Copy the entire source code
COPY . .

# Compile a fully statically linked Go binary with CGO enabled (for sqlite3)
# -linkmode external -extldflags "-static" links musl libc and sqlite3 statically
# -ldflags="-w -s" strips debugging symbols for reduced size
RUN CGO_ENABLED=1 GOOS=linux \
    go build -ldflags="-linkmode external -extldflags '-static' -w -s" -o mcp-api-gateway main.go

# Create a non-root group and user with a fixed, numeric UID/GID
# (We will copy these user database files to the distroless runtime container)
RUN echo "gateway:x:10001:10001:gateway:/app:/sbin/nologin" > /etc/passwd-min && \
    echo "gateway:x:10001:" > /etc/group-min

# Create the data directory for SQLite storage and set permissions for numeric user
RUN mkdir -p /app/data && chown -R 10001:10001 /app/data

# --- Stage 2: Create a secure, minimal run container ---
# Use distroless static image, containing ONLY ca-certificates and tzdata (zero packages/shells)
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Copy passwd and group files to define our custom secure user
COPY --from=builder /etc/passwd-min /etc/passwd
COPY --from=builder /etc/group-min /etc/group

# Copy compiled static binary from the builder stage (owned by root, read-only to user)
COPY --from=builder --chown=root:root /app/mcp-api-gateway /app/mcp-api-gateway

# Copy empty data directory with correct ownership (writable by the non-root user)
COPY --from=builder --chown=10001:10001 /app/data /app/data

# Switch to our secure, numeric non-root user
USER 10001:10001

# Set standard environment defaults (excluding any hardcoded sensitive secrets)
ENV PORT=8080
ENV DATABASE_PATH=/app/data/mcp-gateway.db
ENV VAULT_PROVIDER=local
ENV VAULT_LOCAL_PATH=/app/data/secrets.json

# Expose HTTP / HTTPS port
EXPOSE 8080

# Run the API Gateway
ENTRYPOINT ["/app/mcp-api-gateway"]
