SHELL := bash

# Detect existing Kind cluster at parse time
_KIND_CLUSTERS := $(shell kind get clusters 2>/dev/null)

.PHONY: build build-identity build-notification \
	up down logs clean migrate-add \
	k8s-up k8s-pause k8s-destroy setup-all \
	deploy deploy-identity deploy-notification \
	ingress-install \
	loki-install loki-uninstall monitoring-upgrade \
	dashboard-apply \
	prometheus-install prometheus-uninstall \
	tools-deploy tools-remove

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
ifndef SERVICE
	$(error Usage: make migrate-add SERVICE=identity NAME=create_users_table)
endif
ifndef NAME
	$(error Usage: make migrate-add SERVICE=identity NAME=create_users_table)
endif
	@echo "Creating migration for $(SERVICE)..."
	goose -dir services/$(SERVICE)/migrations create $(NAME) sql

# ==========================================
# Kubernetes: Cluster Lifecycle
# ==========================================

# Smart start: resumes existing cluster or creates + bootstraps a new one
k8s-up:
ifeq ($(filter desktop,$(_KIND_CLUSTERS)),desktop)
	@echo "Cluster 'desktop' found. Resuming..."
	-docker start desktop-control-plane desktop-worker
	@echo "Cluster resumed."
else
	@echo "Creating new cluster 'desktop'..."
	kind create cluster --name desktop --config kind-config.yaml
	$(MAKE) setup-all
endif

# Pause cluster without deleting — containers will not auto-start when Docker Desktop opens
k8s-pause:
	@echo "Pausing Kind cluster..."
	-docker stop desktop-control-plane desktop-worker
	@echo "Cluster paused. Run 'make k8s-up' to resume."

# Full teardown: delete cluster + cleanup all helper containers
k8s-destroy:
	@echo "Destroying Kind cluster..."
	-kind delete cluster --name desktop
	-docker rm -f kind-registry-mirror kind-cloud-provider
	@echo "Cluster destroyed."

# Internal: full bootstrap called by k8s-up on first cluster creation
setup-all: ingress-install prometheus-install loki-install deploy tools-deploy
	@echo "=========================================="
	@echo " Full setup completed!"
	@echo " Grafana:      http://localhost/grafana  (admin/admin123)"
	@echo " Identity:     http://localhost/identity/swagger/index.html"
	@echo " Notify:       http://localhost/notification/swagger/index.html"
	@echo " Adminer:      http://adminer.localhost"
	@echo " RedisInsight: http://redisinsight.localhost"
	@echo "=========================================="

# ==========================================
# Deploy Services (Build → Load into Kind → Restart)
# ==========================================

deploy:
	kubectl apply -f k8s/core/ -R
	kubectl apply -f k8s/infrastructure/ -R
	kubectl apply -f k8s/services/ -R
	kubectl apply -f k8s/tools/ -R
	$(MAKE) deploy-identity deploy-notification
	@echo "All services deployed successfully!"

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

# ==========================================
# Helm - Monitoring Stack
# ==========================================

ingress-install:
	@echo "Installing NGINX Ingress Controller..."
	kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml
	@echo "Waiting for Ingress Controller to be ready..."
	kubectl rollout status deployment/ingress-nginx-controller -n ingress-nginx --timeout=180s

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

# Update Loki/Grafana/Promtail config without rebuilding — run after editing loki-values.yaml
monitoring-upgrade:
	@echo "Upgrading Loki Stack config..."
	helm upgrade loki grafana/loki-stack \
		--namespace monitoring \
		-f helm-values/loki-values.yaml
	@echo "Config upgraded. Grafana will reload dashboards after ~30s."

# Apply Grafana dashboard ConfigMap without a full Helm upgrade
dashboard-apply:
	@echo "Applying Grafana Logs dashboard ConfigMap..."
	kubectl apply -f k8s/monitoring/grafana-logs-dashboard.yaml
	@echo "Dashboard applied. Go to: http://localhost/grafana -> Dashboards -> Microservices"

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
# GUI Tools (Browser-based)
# ==========================================

tools-deploy:
	@echo "Deploying GUI tools (Adminer + RedisInsight)..."
	kubectl apply -f k8s/tools/
	@echo "=========================================="
	@echo " One-time setup — add to hosts file:"
	@echo "   Windows: C:\\Windows\\System32\\drivers\\etc\\hosts"
	@echo "   Linux/Mac: /etc/hosts"
	@echo ""
	@echo "   127.0.0.1 adminer.localhost"
	@echo "   127.0.0.1 redisinsight.localhost"
	@echo ""
	@echo " Then open in browser:"
	@echo "   Adminer (PostgreSQL): http://adminer.localhost"
	@echo "   RedisInsight (Redis): http://redisinsight.localhost"
	@echo "=========================================="

tools-remove:
	@echo "Removing GUI tools..."
	kubectl delete -f k8s/tools/