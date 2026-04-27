.PHONY: build build-identity build-notification up down logs clean migrate-add k8s-apply k8s-delete loki-install loki-uninstall

build: build-identity build-notification

build-identity:
	@echo "Building identity service docker image..."
	docker build -t identity-service -f services/identity/Dockerfile .

build-notification:
	@echo "Building notification service docker image..."
	docker build -t notification-service -f services/notification/Dockerfile .

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

k8s-apply:
	@echo "Applying Kubernetes manifests recursively..."
	kubectl apply -f k8s/ -R

k8s-delete:
	@echo "Deleting Kubernetes manifests recursively..."
	kubectl delete -f k8s/ -R

loki-install:
	@echo "Installing Loki Stack via Helm..."
	helm repo add grafana https://grafana.github.io/helm-charts
	helm repo update
	helm upgrade --install loki grafana/loki-stack \
		--namespace monitoring \
		--create-namespace \
		-f helm-values/loki-values.yaml

loki-uninstall:
	@echo "Uninstalling Loki Stack..."
	helm uninstall loki --namespace monitoring
