# Stage 1: Build Go API and Seed CLI binaries
FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/api ./cmd/api \
 && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/seed ./cmd/seed

# Stage 2: Minimal runtime image
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 10001 appuser

WORKDIR /app
COPY --from=builder /out/api /app/api
COPY --from=builder /out/seed /app/seed
COPY data /app/data
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh && chown -R appuser:appuser /app

USER appuser
ENV GIN_MODE=release
EXPOSE 8080

ENTRYPOINT ["/app/entrypoint.sh"]
