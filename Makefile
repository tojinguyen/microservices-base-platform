.PHONY: build build-identity build-notification \
	up down logs clean migrate-add \
	k8s-apply k8s-delete \
	deploy-identity deploy-notification deploy-all \
	loki-install loki-uninstall monitoring-upgrade \
	dashboard-apply \
	prometheus-install prometheus-uninstall \
	ingress-install cluster-up cluster-down \
	setup-all

# ==========================================
# Docker Compose (local dev)
# ==========================================

up:
	@echo "Starting all services with docker-compose..."
	docker compose up -d

down:
	@echo "Stopping all services..."
	docker compose down

logs:
	docker compose logs -f

# ==========================================
# Build Docker Images
# ==========================================

build: build-identity build-notification

build-identity:
	@echo "Building identity service docker image..."
	docker build -t identity-service -f services/identity/Dockerfile .

build-notification:
	@echo "Building notification service docker image..."
	docker build -t notification-service -f services/notification/Dockerfile .

# ==========================================
# Database Migration
# Usage: make migrate-add SERVICE=identity NAME=create_users_table
# ==========================================

migrate-add:
	@if [ -z "$(SERVICE)" ] || [ -z "$(NAME)" ]; then \
		echo "Usage: make migrate-add SERVICE=identity NAME=create_users_table"; \
		exit 1; \
	fi
	@echo "Creating migration for $(SERVICE)..."
	goose -dir services/$(SERVICE)/migrations create $(NAME) sql

# ==========================================
# Kubernetes Manifests
# ==========================================

k8s-apply:
	@echo "Applying Kubernetes manifests recursively..."
	kubectl apply -f k8s/ -R

k8s-delete:
	@echo "Deleting Kubernetes manifests recursively..."
	kubectl delete -f k8s/ -R

# ==========================================
# Deploy Services (Build → Load into Kind → Restart)
# ==========================================

deploy-identity: build-identity
	@echo "Loading identity-service image into kind cluster..."
	kind load docker-image identity-service:latest --name desktop
	kubectl rollout restart deployment/identity-deployment -n microservices-platform
	kubectl rollout status deployment/identity-deployment -n microservices-platform

deploy-notification: build-notification
	@echo "Loading notification-service image into kind cluster..."
	kind load docker-image notification-service:latest --name desktop
	kubectl rollout restart \
		deployment/notification-api \
		deployment/notification-worker-email \
		deployment/notification-worker-pending \
		deployment/notification-worker-webhook \
		-n microservices-platform

# Apply K8s manifests + Build & Deploy all services
deploy-all: k8s-apply deploy-identity deploy-notification
	@echo "All services deployed successfully!"

# ==========================================
# Helm - Monitoring Stack
# ==========================================

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

# Cập nhật cấu hình Loki/Grafana/Promtail mà không cần rebuild image
# Dùng sau khi sửa loki-values.yaml (vd: thêm pipeline stage, dashboard provider)
monitoring-upgrade:
	@echo "Upgrading Loki Stack config (Promtail pipeline + Grafana)..."
	helm upgrade loki grafana/loki-stack \
		--namespace monitoring \
		-f helm-values/loki-values.yaml
	@echo "Config upgraded. Grafana sẽ tự load dashboard mới sau ~30s."

# Áp dụng nhanh Grafana dashboard ConfigMap mà không cần upgrade toàn bộ helm
dashboard-apply:
	@echo "Applying Grafana Logs dashboard ConfigMap..."
	kubectl apply -f k8s/monitoring/grafana-logs-dashboard.yaml
	@echo "Dashboard applied! Truy cập: http://localhost/grafana -> Dashboards -> Microservices"

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

# ==========================================
# Setup All (Full bootstrap - chạy lần đầu sau cluster-up)
# Thứ tự: Ingress → Prometheus → Loki → Deploy services
# ==========================================

setup-all: ingress-install prometheus-install loki-install deploy-all
	@echo "=========================================="
	@echo " Full setup completed successfully!"
	@echo " Grafana:  http://localhost/grafana  (admin/admin123)"
	@echo " Identity: http://localhost/identity/swagger/index.html"
	@echo " Notify:   http://localhost/notification/swagger/index.html"
	@echo "=========================================="

# ==========================================
# Utilities
# ==========================================

clean:
	@echo "Cleaning up build artifacts..."
	rm -rf bin/
