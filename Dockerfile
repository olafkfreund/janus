# Stage 1: Build the Go backend binary
FROM golang:1.22-alpine AS builder

# Install build dependencies for CGO (required by go-sqlite3)
RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

# Copy dependency files
COPY go.mod ./
# If go.sum exists, copy it (it will be created during module tidy)
COPY go.sum* ./

# Download dependencies (highly cached in Docker layer)
RUN go mod download

# Copy the entire source code
COPY . .

# Compile statically linked Go binary with CGO enabled (for sqlite3)
# -ldflags="-w -s" strips debugging symbols for reduced size
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-w -s" -o mcp-api-gateway main.go

# Stage 2: Create a secure, minimal run container
FROM alpine:3.19

# Install ca-certificates and sqlite library for execution
RUN apk add --no-cache ca-certificates sqlite-libs

WORKDIR /app

# Copy compiled binary from the builder stage
COPY --from=builder /app/mcp-api-gateway /app/mcp-api-gateway

# Set standard environment defaults
ENV PORT=8080
ENV DATABASE_PATH=/app/data/mcp-gateway.db
ENV VAULT_PROVIDER=local
ENV VAULT_LOCAL_PATH=/app/data/secrets.json
ENV JWT_SECRET=change-this-in-production-to-something-secure
ENV GATEWAY_TOKEN=secure-mcp-gateway-token-123456

# Create a data directory for SQLite database storage
RUN mkdir -p /app/data

# Run as non-root user for security (enterprise compliance)
RUN addgroup -S gateway && adduser -S gateway -G gateway
RUN chown -R gateway:gateway /app
USER gateway

# Expose HTTP / HTTPS port
EXPOSE 8080

# Run the API Gateway
CMD ["/app/mcp-api-gateway"]
