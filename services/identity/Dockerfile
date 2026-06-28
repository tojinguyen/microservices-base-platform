# Stage 1: Build
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

RUN go install github.com/swaggo/swag/cmd/swag@latest

COPY pkg/go.mod ./pkg/
COPY services/identity/go.mod services/identity/go.sum ./services/identity/

RUN go work init && \
    go work use ./pkg ./services/identity

RUN cd pkg && go mod download
RUN cd services/identity && go mod download


COPY pkg/ ./pkg/
COPY services/identity/ ./services/identity/

RUN cd services/identity && swag init -g cmd/main.go --parseDependency --parseInternal

RUN CGO_ENABLED=0 GOOS=linux go build -o identity-service ./services/identity/cmd/main.go

# Stage 2: Final image
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /root

COPY --from=builder /app/identity-service ./
RUN chmod +x ./identity-service

EXPOSE 8080
CMD ["./identity-service"]