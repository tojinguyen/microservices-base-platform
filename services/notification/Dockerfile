# Stage 1: Build
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.work go.work.sum ./
COPY pkg/ ./pkg/
COPY services/notification/go.mod ./services/notification/

RUN cd services/notification && go mod download

COPY services/notification/migrations/ ./services/notification/migrations/
COPY services/notification/internal/ ./services/notification/internal/
COPY services/notification/cmd/ ./services/notification/cmd/

RUN CGO_ENABLED=0 GOOS=linux go build -o notification-service ./services/notification/cmd/main.go

# Stage 2: Final image
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /root

COPY --from=builder /app/notification-service ./
RUN chmod +x ./notification-service

EXPOSE 8082
CMD ["./notification-service"]
