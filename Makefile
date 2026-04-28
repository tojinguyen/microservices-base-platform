.PHONY: build build-identity build-notification up down logs clean migrate-add \
	k8s-apply k8s-delete deploy-all setup-all \
	loki-install loki-uninstall prometheus-install prometheus-uninstall \
	ingress-install cluster-up cluster-down

build: build-identity build-notification

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

k8s-apply:
	@echo "Applying Kubernetes manifests recursively..."
	kubectl apply -f k8s/ -R

k8s-delete:
	@echo "Deleting Kubernetes manifests recursively..."
	kubectl delete -f k8s/ -R

# Build + Apply manifests + Deploy services
deploy-all: k8s-apply deploy-identity deploy-notification
	@echo "All services deployed successfully!"

# Helm - Monitoring Stack
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

prometheus-install:
	@echo "Installing Prometheus (kube-prometheus-stack) via Helm..."
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
	helm repo update
	helm upgrade --install prometheus prometheus-community/kube-prometheus-stack \
		--namespace monitoring \
		--create-namespace \
		-f helm-values/prometheus-values.yaml

prometheus-uninstall:
	@echo "Uninstalling Prometheus..."
	helm uninstall prometheus --namespace monitoring

# Local Cluster Management (Kind)
cluster-up:
	@echo "Creating Kind cluster with ingress port mapping..."
	kind create cluster --name desktop --config kind-config.yaml

cluster-down:
	@echo "Deleting Kind cluster..."
	kind delete cluster --name desktop
	@echo "Cleaning up helper containers (registry mirror, cloud provider)..."
	docker rm -f kind-registry-mirror kind-cloud-provider || true

ingress-install:
	@echo "Installing NGINX Ingress Controller..."
	kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml
	@echo "Waiting for Ingress Controller to be ready..."
	kubectl wait --namespace ingress-nginx \
		--for=condition=ready pod \
		--selector=app.kubernetes.io/component=controller \
		--timeout=90s

# Setup All
setup-all: ingress-install prometheus-install loki-install deploy-all
	@echo "=========================================="
	@echo " Full setup completed successfully!"
	@echo " Grafana:  http://localhost  (admin/admin123)"
	@echo " Identity: http://localhost/identity/swagger/index.html"
	@echo " Notify:   http://localhost/notification/swagger/index.html"
	@echo "=========================================="
