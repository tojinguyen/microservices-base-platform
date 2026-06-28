# Stage 1: Build
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

RUN go install github.com/swaggo/swag/cmd/swag@latest

COPY pkg/go.mod ./pkg/
COPY services/notification/go.mod ./services/notification/

RUN go work init && \
    go work use ./pkg ./services/notification

RUN cd pkg && go mod download
RUN cd services/notification && go mod download

COPY pkg/ ./pkg/
COPY services/notification/ ./services/notification/

RUN cd services/notification && swag init -g cmd/main.go --parseDependency --parseInternal

RUN CGO_ENABLED=0 GOOS=linux go build -o notification-service ./services/notification/cmd/main.go

# Stage 2: Final image
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /root

COPY --from=builder /app/notification-service ./
RUN chmod +x ./notification-service

EXPOSE 8082
CMD ["./notification-service"]
