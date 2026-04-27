# microservices-base-platform

Nền tảng microservices cơ bản gồm **Identity Service** và **Notification Service**, triển khai trên Kubernetes (kind).

---

## 🏗️ Kiến trúc

```
Ingress (nginx)
    ├── /api/v1/auth/*           → identity-service:8080
    └── /api/v1/notifications/*  → notification-service:8082

identity-service  → postgres-identity
                  → redis (JWT)

notification-service (API + 3 Workers)
    ├── notification-api         → postgres-notification, rabbitmq, mailpit
    ├── notification-worker-pending
    ├── notification-worker-email
    └── notification-worker-webhook
```

---

## 📋 Services

| Service | Port | Mô tả |
|---|---|---|
| identity-service | 8080 | Xác thực, quản lý người dùng |
| notification-service | 8082 | Gửi thông báo qua email/webhook |
| postgres-identity | 5432 | Database cho Identity |
| postgres-notification | 5432 | Database cho Notification |
| rabbitmq | 5672 / 15672 | Message broker |
| mailpit | 1025 / 8025 | SMTP test server |

---

## 📖 API Documentation (Swagger)

### Cách 1: Qua Ingress (sau khi deploy K8s)

| Service | URL |
|---|---|
| Identity Service | http://localhost/identity/swagger/index.html |
| Notification Service | http://localhost/notification/swagger/index.html |

### Cách 2: Port-forward trực tiếp đến Pod

```bash
# Identity Service (Service dùng port 8081)
kubectl port-forward svc/identity-service 8080:8081 -n microservices-platform
# Truy cập: http://localhost:8080/swagger/index.html

# Notification Service
kubectl port-forward svc/notification-service 8082:8082 -n microservices-platform
# Truy cập: http://localhost:8082/swagger/index.html

# RabbitMQ Management UI
kubectl port-forward svc/rabbitmq-service 15672:15672 -n microservices-platform
# Truy cập: http://localhost:15672 (user: guest / pass: guest)

# Mailpit UI (xem email test)
kubectl port-forward svc/mailpit-service 8025:8025 -n microservices-platform
# Truy cập: http://localhost:8025
```

---

## 🚀 Deploy lên Kubernetes (kind)

### Yêu cầu
- Docker Desktop
- kind
- kubectl

### Bước 1: Khởi tạo Cluster với cấu hình Port
Để đảm bảo `localhost` nhận được traffic qua Ingress, ta cần tạo cluster với cấu hình map port (đã được tích hợp sẵn trong lệnh Make):

```bash
make cluster-up
```

### Bước 2: Cài đặt Nginx Ingress Controller

```bash
make ingress-install
```

### Bước 3: Deploy toàn bộ hệ thống
Lệnh này sẽ tự động build image, load vào kind cluster và apply toàn bộ YAML config:

```bash
make deploy-all
```

### Bước 4: Cài đặt hệ thống Monitoring (Tùy chọn)

```bash
make loki-install
make prometheus-install
```

### Kiểm tra trạng thái

```bash
kubectl get pods -A
```

---

## 🗂️ Cấu trúc thư mục K8s

```
k8s/
├── 00_namespace.yaml            # Namespace: microservices-platform
├── 01_identity_config.yaml      # ConfigMap cho Identity
├── 02_identity_secret.yaml      # Secret cho Identity (gitignore)
├── 03_postgres_identity.yaml    # PostgreSQL cho Identity
├── 04_identity.yaml             # Identity Service Deployment
├── 05_secret_notification.yaml  # Secret cho Notification (gitignore)
├── 06_postgres_notification.yaml # PostgreSQL cho Notification
├── 07_rabbitmq.yaml             # RabbitMQ
├── 08_mailpit.yaml              # Mailpit (SMTP test)
├── 09_notification.yaml         # Notification Service (API + 3 Workers)
└── 10_ingress.yaml              # Nginx Ingress rules
```