.PHONY: build build-identity build-notification up down logs clean migrate-add \
	k8s-apply k8s-delete k8s-load-identity k8s-load-notification \
	k8s-reload-identity k8s-reload-notification deploy-all \
	loki-install loki-uninstall prometheus-install prometheus-uninstall

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

# Load image local vào kind cluster (chạy sau khi build)
k8s-load-identity:
	@echo "Loading identity-service image into kind cluster..."
	kind load docker-image identity-service:latest --name desktop

k8s-load-notification:
	@echo "Loading notification-service image into kind cluster..."
	kind load docker-image notification-service:latest --name desktop

# Build + Load + Restart identity service
k8s-reload-identity: build-identity k8s-load-identity
	@echo "Restarting identity-deployment..."
	kubectl rollout restart deployment/identity-deployment -n microservices-platform
	kubectl rollout status deployment/identity-deployment -n microservices-platform

# Build + Load + Restart notification service
k8s-reload-notification: build-notification k8s-load-notification
	@echo "Restarting notification deployments..."
	kubectl rollout restart deployment/notification-api -n microservices-platform
	kubectl rollout restart deployment/notification-worker-email -n microservices-platform
	kubectl rollout restart deployment/notification-worker-pending -n microservices-platform
	kubectl rollout restart deployment/notification-worker-webhook -n microservices-platform

# Build + Load + Restart toàn bộ services
deploy-all: k8s-apply k8s-reload-identity k8s-reload-notification
	@echo "All services deployed successfully!"

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

# ==========================================
# Local Cluster Management (Kind)
# ==========================================
cluster-up:
	@echo "Creating Kind cluster with ingress port mapping..."
	kind create cluster --name desktop --config kind-config.yaml

cluster-down:
	@echo "Deleting Kind cluster..."
	kind delete cluster --name desktop

ingress-install:
	@echo "Installing NGINX Ingress Controller..."
	kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml
	@echo "Waiting for Ingress Controller to be ready..."
	kubectl wait --namespace ingress-nginx --for=condition=ready pod --selector=app.kubernetes.io/component=controller --timeout=90s

