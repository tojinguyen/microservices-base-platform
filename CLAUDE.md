# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A Go microservices platform with two services: **Identity** (auth/JWT/OAuth) and **Notification** (email/webhook delivery via RabbitMQ). Shared infrastructure lives in a `pkg/` module consumed by both services.

Go workspace: `go.work` manages three modules — `./pkg`, `./services/identity`, `./services/notification`.

## Development Commands

### Local Development (Docker Compose)

```bash
make up          # Start all services + dependencies
make down        # Stop all services
make logs        # Tail logs from all containers
```

### Building Docker Images

```bash
make build                   # Build both services
make build-identity
make build-notification
```

### Database Migrations

```bash
# Add a new migration file
make migrate-add SERVICE=identity NAME=add_user_roles
make migrate-add SERVICE=notification NAME=add_delivery_log

# Migrations run automatically on service startup
```

Migrations use `goose` v3 with SQL files in `services/<name>/migrations/`. Mark up/down blocks with `-- +goose Up` / `-- +goose Down`.

### Running Tests

```bash
# From repo root (uses go workspace)
go test ./...

# Single service
cd services/identity && go test ./...

# Single package with verbose output
cd services/identity && go test ./internal/service/... -v

# Single test by name
cd services/identity && go test ./internal/service/... -run TestAuthService_Register
```

### Swagger Docs

Swagger is generated during Docker build (`swag init`). To regenerate locally:

```bash
cd services/identity && swag init -g cmd/main.go
cd services/notification && swag init -g cmd/main.go
```

### Kubernetes (Kind cluster)

```bash
make cluster-up              # Create kind cluster
make ingress-install         # Install NGINX Ingress Controller
make deploy-all              # Apply manifests + build + load + restart pods
make setup-all               # Full bootstrap: ingress + monitoring + deploy-all

make deploy-identity         # Rebuild and redeploy identity only
make deploy-notification     # Rebuild and redeploy notification only (4 pods)

make prometheus-install      # kube-prometheus-stack
make loki-install            # Loki log aggregation
make dashboard-apply         # Grafana dashboard ConfigMap
```

## Architecture

### Service Map

```
Nginx Ingress (localhost:80)
  /api/v1/auth/*              → identity-service:8080
  /api/v1/notifications/*     → notification-service:8082
  /identity/swagger/*         → identity-service:8080
  /notification/swagger/*     → notification-service:8082
```

- **Identity Service** — `services/identity/` — User registration, login, JWT (access + refresh), Google OAuth, Prometheus metrics at `/metrics`.
- **Notification Service** — `services/notification/` — REST API + three worker modes controlled by `APP_MODE` env var (`api`, `worker-pending`, `worker-email`, `worker-webhook`). Workers pull jobs from RabbitMQ exchange `notification.direct`.

### Shared `pkg/` Module

| Package | Purpose |
|---------|---------|
| `pkg/auth` | JWT generation/validation + Gin middleware |
| `pkg/broker` | RabbitMQ client abstraction (interface-based) |
| `pkg/config` | Viper-based env-var config loading |
| `pkg/db` | GORM + PostgreSQL connection, `goose` migrations runner |
| `pkg/errors` | Custom error types |
| `pkg/logger` | Zap structured logger (JSON, RFC3339Nano, includes `service`/`env` fields) |
| `pkg/redis` | go-redis/v9 wrapper |
| `pkg/response` | Standardized Gin JSON responses |

### Notification Worker Flow

1. `worker-pending` — polls notification_db for pending notifications, publishes to RabbitMQ
2. `worker-email` — consumes queue, sends via SMTP (Mailpit locally)
3. `worker-webhook` — consumes queue, delivers to webhook URLs

Retry logic is built into the `notifications` table (`next_retry_at` column, retry counter).

### Infrastructure (local Docker Compose)

| Service | Port |
|---------|------|
| identity-service | 8080 |
| notification-service | 8082 |
| PostgreSQL (identity_db) | 5432 |
| PostgreSQL (notification_db) | 5433 |
| Redis | 6379 |
| RedisInsight UI | 5540 |
| RabbitMQ | 5672 (AMQP), 15672 (mgmt UI) |
| Mailpit SMTP | 1025 |
| Mailpit UI | 8025 |

### Kubernetes Layout

```
k8s/
  core/           namespace, ingress
  infrastructure/ rabbitmq, mailpit
  services/
    identity/     ConfigMap, Secret, PostgreSQL, Redis, Deployment
    notification/ Secret, PostgreSQL, 4 Deployments (api + 3 workers)
  monitoring/     Grafana dashboard ConfigMap
```

## Coding Conventions

- Follow the Standard Go Project Layout (`cmd/`, `internal/`, `pkg/`).
- Write comments and documentation in **English**.
- Use `zap` for structured logging — never `fmt.Println` in production paths.
- Config is loaded exclusively from environment variables via `pkg/config` (Viper).
- All HTTP handlers live in `internal/handler/`, business logic in `internal/service/`, DB access in `internal/repository/`.
- Repository interfaces are defined in `internal/repository/` and mocked under `internal/repository/mocks/` using testify mocks for unit tests.
- Database connection pool defaults (in `pkg/db`): max 25 open, 10 idle, 1-hour lifetime — adjust per service load.
- Multi-stage Dockerfiles: build stage uses `golang:1.26-alpine` (runs `swag init`), runtime stage uses `alpine:latest`.
