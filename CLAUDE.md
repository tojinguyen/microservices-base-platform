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
make k8s-up                  # Create kind cluster (or resume existing)
make k8s-pause               # Pause cluster without deleting
make k8s-destroy             # Full teardown
make ingress-install         # Install NGINX Ingress Controller
make deploy                  # Apply manifests + build + load + restart pods
make setup-all               # Full bootstrap: ingress + monitoring + deploy + tools

make deploy-identity         # Rebuild and redeploy identity only
make deploy-notification     # Rebuild and redeploy notification only (4 pods)

make prometheus-install      # kube-prometheus-stack
make loki-install            # Loki log aggregation
make dashboard-apply         # Grafana dashboard ConfigMap

make tools-deploy            # Deploy GUI tools (Adminer + RedisInsight)
make tools-remove            # Remove GUI tools
```

GUI tools require a one-time hosts file entry (see [GUI Tools](#gui-tools-kubernetes) below).

## Architecture

### Service Map

```
Nginx Ingress (localhost:80)
  /api/v1/auth/*              → identity-service:8080
  /api/v1/notifications/*     → notification-service:8082
  /identity/swagger/*         → identity-service:8080
  /notification/swagger/*     → notification-service:8082

Nginx Ingress — host-based (GUI tools)
  cloudbeaver.localhost       → cloudbeaver:8978    (PostgreSQL GUI)
  redisinsight.localhost      → redisinsight:5540   (Redis GUI)
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
  tools/          Adminer, RedisInsight, tools-ingress (GUI tools)
```

### GUI Tools (Kubernetes)

Browser-based tools deployed to the cluster for inspecting data without CLI.

**One-time hosts file setup** (`C:\Windows\System32\drivers\etc\hosts` on Windows):
```
127.0.0.1 adminer.localhost
127.0.0.1 redisinsight.localhost
```

**CloudBeaver** — `http://cloudbeaver.localhost` — web-based DBeaver (PostgreSQL GUI)
- Identity: server `postgres-identity-service:5432` → `identity_db` / `user_admin` / `password123`
- Notification: server `postgres-notification-service:5432` → `notification_db` / `user_admin` / `password123`

**RedisInsight** — `http://redisinsight.localhost` — Redis web UI. Add connections manually:
- Identity Redis: host `redis-identity-service`, port `6379`
- Notification Redis: host `redis-notification-service`, port `6379`

## Coding Conventions

- Follow the Standard Go Project Layout (`cmd/`, `internal/`, `pkg/`).
- Write comments and documentation in **English**.
- Use `zap` for structured logging — never `fmt.Println` in production paths.
- Config is loaded exclusively from environment variables via `pkg/config` (Viper).
- All HTTP handlers live in `internal/handler/`, business logic in `internal/service/`, DB access in `internal/repository/`.
- Repository interfaces are defined in `internal/repository/` and mocked under `internal/repository/mocks/` using testify mocks for unit tests.
- Database connection pool defaults (in `pkg/db`): max 25 open, 10 idle, 1-hour lifetime — adjust per service load.
- Multi-stage Dockerfiles: build stage uses `golang:1.26-alpine` (runs `swag init`), runtime stage uses `alpine:latest`.

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **microservices-base-platform** (2480 symbols, 5474 relationships, 198 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> Index stale? Run `node .gitnexus/run.cjs analyze` from the project root — it auto-selects an available runner. No `.gitnexus/run.cjs` yet? `npx gitnexus analyze` (npm 11 crash → `npm i -g gitnexus`; #1939).

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows. For regression review, compare against the default branch: `detect_changes({scope: "compare", base_ref: "main"})`.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `query({query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `context({name: "symbolName"})`.

## Never Do

- NEVER edit a function, class, or method without first running `impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `rename` which understands the call graph.
- NEVER commit changes without running `detect_changes()` to check affected scope.

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/microservices-base-platform/context` | Codebase overview, check index freshness |
| `gitnexus://repo/microservices-base-platform/clusters` | All functional areas |
| `gitnexus://repo/microservices-base-platform/processes` | All execution flows |
| `gitnexus://repo/microservices-base-platform/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->
