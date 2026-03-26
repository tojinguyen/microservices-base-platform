# Project Guidelines

## Code Style
- Written in **Go** using a multi-module workspace (`go.work`).
- Adhere strictly to idiomatic Go formatting and the Standard Go Project Layout patterns.

## Architecture
- **Shared Libraries (`pkg/`)**: Contains centralized functionality for authentication, database (PostgreSQL), message broker (RabbitMQ), caching (Redis), error handling, and standardized API responses.
- **Microservices (`services/`)**: Each service follows Clean Architecture, organized into components: 
  - `cmd/`: Application entry point.
  - `internal/domain/`: Core business models and interfaces.
  - `internal/dto/`: Data transfer objects for request/response bodies.
  - `internal/repository/`: Data layer, executing queries.
  - `internal/service/`: Core business logic.
  - `internal/handler/`: HTTP layer (handling requests/responses).
  - `internal/route/`: Route definitions and middleware attachments.

## Build and Run
- **Orchestration**: Run the entire stack locally using `docker compose up -d` (which wires up PostgreSQL, Redis, RabbitMQ, and your services automatically on their shared network).
- **Makefile**: Utilize `make up`, `make down`, `make build`, and `make logs` for quick command access.

## Conventions
- **Reuse Shared Packages**: NEVER rebuild basic utilities for database transactions, logging, Redis caching, or AMQP brokering inside individual services. Always import and utilize the corresponding package from `pkg/`.
- **Standardized Responses**: Always use the `pkg/response` wrapper for sending HTTP responses to ensure platform-wide consistency.
- **Config Management**: Services should parse their own configuration relying on structures similar to `pkg/config`.
