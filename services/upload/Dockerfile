# Stage 1: Build
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

RUN go install github.com/swaggo/swag/cmd/swag@latest

COPY pkg/go.mod ./pkg/
COPY services/upload/go.mod ./services/upload/

RUN go work init && \
    go work use ./pkg ./services/upload

RUN cd pkg && go mod download
RUN cd services/upload && go mod download

COPY pkg/ ./pkg/
COPY services/upload/ ./services/upload/

RUN cd services/upload && swag init -g cmd/main.go --parseDependency --parseInternal

RUN CGO_ENABLED=0 GOOS=linux go build -o upload-service ./services/upload/cmd/main.go

# Stage 2: Final image
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /root

COPY --from=builder /app/upload-service ./
RUN chmod +x ./upload-service

EXPOSE 8083
CMD ["./upload-service"]
