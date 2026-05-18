FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o bsc-scan ./cmd/bsc-scan
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o bsc-admin ./admin

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata curl
COPY --from=builder /app/bsc-scan /usr/local/bin/
COPY --from=builder /app/bsc-admin /usr/local/bin/
COPY --from=builder /app/migrations /migrations
COPY --from=builder /app/frontend /etc/bsc-scan/frontend
COPY --from=builder /app/config.yaml /etc/bsc-scan/config.yaml
WORKDIR /etc/bsc-scan
EXPOSE 8080
CMD ["bsc-admin"]
