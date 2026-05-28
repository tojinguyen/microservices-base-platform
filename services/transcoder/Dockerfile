# Stage 1: Build
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY pkg/go.mod ./pkg/
COPY services/transcoder/go.mod ./services/transcoder/

RUN go work init && \
    go work use ./pkg ./services/transcoder

RUN cd pkg && go mod download
RUN cd services/transcoder && go mod download

COPY pkg/ ./pkg/
COPY services/transcoder/ ./services/transcoder/

RUN CGO_ENABLED=0 GOOS=linux go build -o transcoder-service ./services/transcoder/cmd/main.go

# Stage 2: Runtime — needs FFmpeg binary
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata ffmpeg
WORKDIR /root

COPY --from=builder /app/transcoder-service ./
RUN chmod +x ./transcoder-service

ENTRYPOINT ["./transcoder-service"]
