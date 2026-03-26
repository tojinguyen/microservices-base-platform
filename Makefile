.PHONY: build-identity up down logs clean

build-identity:
	@echo "Building identity service docker image..."
	docker build -t identity-service -f services/identity/Dockerfile .

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
