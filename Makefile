.PHONY: build-identity up down logs clean migrate-add

build-identity:
	@echo "Building identity service docker image..."
	docker build -t identity-service -f services/identity/Dockerfile .

# Migration Helper
# Usage: make migrate-add SERVICE=identity NAME=create_users_table
migrate-add:
	@if [ -z "$(SERVICE)" ] || [ -z "$(NAME)" ]; then \
		echo "Usage: make migrate-add SERVICE=identity NAME=create_users_table"; \
		exit 1; \
	fi
	@echo "Creating migration for $(SERVICE)..."
	goose -dir services/$(SERVICE)/migrations create $(NAME) sql

up:
	@echo "Starting all services with docker-compose..."
	docker compose up -d

down:
	@echo "Stopping all services..."
	docker compose down

logs:
	docker compose logs -f

clean:
	@echo "Cleaning up..."
	rm -rf bin/
