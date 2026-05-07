# ---- Builder Stage ----
FROM golang:1.25-alpine AS builder

RUN apk update && apk add --no-cache git build-base ca-certificates
RUN update-ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY client/ ./client/
COPY service/ ./service/

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/forohtoo cmd/forohtoo/*.go
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/server cmd/server/*.go

# ---- Final Stage ----
FROM alpine:latest

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

WORKDIR /app

COPY --from=builder /bin/forohtoo /forohtoo
COPY --from=builder /bin/server /server

EXPOSE 8080

CMD ["/server"]
