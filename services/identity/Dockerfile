# Stage 1: Build
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.work go.work.sum ./
COPY pkg/ ./pkg/
COPY services/identity/go.mod services/identity/go.sum ./services/identity/

COPY services/identity/migrations/ ./services/identity/migrations/
COPY services/identity/internal/ ./services/identity/internal/
COPY services/identity/cmd/ ./services/identity/cmd/

RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux go build -o identity-service ./services/identity/cmd/main.go

# Stage 2: Final image
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /root

COPY --from=builder /app/identity-service ./
RUN chmod +x ./identity-service

EXPOSE 8080
CMD ["./identity-service"]