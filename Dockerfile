# ---- Builder Stage ----
FROM golang:1.25-alpine AS builder

# Install build dependencies AND CA certificates
RUN apk update && apk add --no-cache git build-base ca-certificates
RUN update-ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod and sum files FIRST
COPY go.mod go.sum ./
# Download dependencies - this layer is cached if go.mod/go.sum don't change
RUN go mod download

# Copy ONLY the necessary source directories
COPY cmd/ ./cmd/
COPY client/ ./client/
COPY service/ ./service/

# Build the application binary statically for Linux
# Use ldflags to strip debug info and reduce binary size
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/forohtoo cmd/forohtoo/*.go
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/server cmd/server/*.go
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/worker cmd/worker/*.go

# ---- Final Stage ----
FROM alpine:latest

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

WORKDIR /app

# Copy the built binaries from the builder stage
COPY --from=builder /bin/forohtoo /forohtoo
COPY --from=builder /bin/server /server
COPY --from=builder /bin/worker /worker

# Expose the port the server listens on
EXPOSE 8080

# Set the default entrypoint to run the server
# Kubernetes deployments should override this with command for worker
CMD ["/server"]
