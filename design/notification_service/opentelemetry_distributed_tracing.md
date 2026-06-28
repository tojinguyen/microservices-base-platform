# Thiết Kế & Kế Hoạch Triển Khai: OpenTelemetry Distributed Tracing

Tài liệu này lưu trữ chi tiết thiết kế kiến trúc và kế hoạch kỹ thuật để triển khai **OpenTelemetry Distributed Tracing** cho **Notification Service** trong hệ thống. Hệ thống sẽ trace toàn bộ hành trình gửi thông báo: từ API request -> Outbox Event (Database) -> Outbox Worker -> RabbitMQ -> Email/Webhook Worker -> SMTP/Mailpit Webhook.

---

## 1. Kiến Trúc Distributed Tracing

Để trace được một tiến trình xử lý bất đồng bộ đi qua nhiều ranh giới công nghệ khác nhau (HTTP, Database, Broker), chúng ta cần giải quyết bài toán truyền dẫn Trace Context (Context Propagation).

### Sơ đồ luồng dữ liệu & Trace Context
```mermaid
sequenceDiagram
    autonumber
    actor User as Client
    participant API as Notification API (Gin)
    database DB as Postgres DB (Outbox Table)
    participant OutWorker as Outbox Worker (Go)
    participant RMQ as RabbitMQ Broker
    participant EmailWorker as Email Worker (Go)
    participant Mailpit as SMTP Server (Mailpit)
    participant Webhook as Webhook Handler (Gin)

    User->>API: HTTP POST /send (Trace Parent được tự sinh hoặc trích xuất từ HTTP Headers)
    Note over API: Khởi tạo Root Span: "POST /send"<br/>Tạo child span: "service.create_notification"
    API->>DB: Ghi Outbox Event (Lưu JSON trace_context vào cột payload)
    API-->>User: Phản hồi 202 Accepted
    
    Note over OutWorker: Quét DB tìm event pending
    OutWorker->>DB: Query & Claim Events
    Note over OutWorker: Giải mã payload.trace_context<br/>Khôi phục Context & Tạo child span: "outbox.publish"
    OutWorker->>RMQ: Publish Message (Inject Context vào AMQP Headers)
    
    Note over EmailWorker: Nhận tin từ Queue
    RMQ->>EmailWorker: Consume Message
    Note over EmailWorker: Trích xuất Context từ AMQP Headers<br/>Tạo child span: "worker.email.process"
    EmailWorker->>Mailpit: Gửi Email qua SMTP (Tạo span: "worker.email.send_smtp")
    
    Note over Mailpit: Nhận email & gửi Webhook callback
    Mailpit->>Webhook: HTTP POST /webhook (Truyền traceparent qua Headers)
    Note over Webhook: Trích xuất Context từ HTTP Headers<br/>Tạo span: "HandleMailpitWebhook"
```

---

## 2. Giải Pháp Kỹ Thuật Chi Tiết

### A. Truyền dẫn Context qua Database (Transactional Outbox)
Vì Outbox Pattern lưu sự kiện vào DB trước rồi mới quét để publish sau, trace context nguyên bản của HTTP request sẽ bị mất nếu không được lưu trữ.
- **Giải pháp:** Tuần tự hóa trace context thành `map[string]string` và chèn trực tiếp vào trường `trace_context` trong cột `payload` (JSONB) của bảng `outbox_events`.
- **Cách phục hồi:** Khi Outbox Worker đọc bản ghi lên, nó giải mã trường này và phục hồi lại `context.Context` hoạt động để tiếp tục chuỗi span.

### B. Truyền dẫn Context qua RabbitMQ (AMQP Propagation)
RabbitMQ hỗ trợ trường `Headers` kiểu `amqp.Table` (`map[string]interface{}`).
- **Giải pháp:** Xây dựng `AMQPHeadersCarrier` implement interface `propagation.TextMapCarrier` của OpenTelemetry.
- **Publish:** Inject context hiện tại vào header `traceparent` của tin nhắn gửi đi.
- **Consume:** Extract `traceparent` từ header của tin nhắn nhận được để khôi phục trace context trước khi gọi hàm handler.

---

## 3. Danh Sách Các File Cần Chỉnh Sửa & Tạo Mới

### A. Shared Package (Module `pkg`)

#### 1. [NEW] `pkg/trace/trace.go`
Đóng gói toàn bộ logic cấu hình OpenTelemetry:
- Khởi tạo Tracer Provider kết nối tới Jaeger qua OTLP HTTP/gRPC.
- Viết custom middleware `TracerMiddleware` cho Gin framework (tự động tạo span, trích xuất HTTP headers, ghi nhận lỗi và HTTP status).
- Viết `AMQPHeadersCarrier` cho RabbitMQ.
- Viết helper `InjectMap` và `ExtractMap` phục vụ lưu/đọc trace context qua JSON database.

#### 2. [MODIFY] `pkg/go.mod`
Thêm các thư viện OpenTelemetry chuẩn:
- `go.opentelemetry.io/otel`
- `go.opentelemetry.io/otel/trace`
- `go.opentelemetry.io/otel/sdk`
- `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`

#### 3. [MODIFY] `pkg/broker/rabbitmq.go`
- Hàm `Publish`: Tự động inject context hiện tại vào `amqp.Publishing.Headers`.
- Hàm `handleMessages`: Trích xuất context từ tin nhắn nhận được bằng `AMQPHeadersCarrier` và gọi handler thực thi với context mới.

---

### B. Dịch Vụ Notification (Module `services/notification`)

#### 1. [MODIFY] `services/notification/go.mod`
Thêm package `backend/pkg/trace` và các thư viện OpenTelemetry.

#### 2. [MODIFY] `services/notification/internal/config/config.go`
Bổ sung các tham số cấu hình:
- `OTEL_ENABLED` (Bật/tắt tracing, mặc định `true`).
- `OTEL_EXPORTER_OTLP_ENDPOINT` (Địa chỉ Jaeger OTLP, mặc định `http://jaeger:4318`).

#### 3. [MODIFY] `services/notification/cmd/main.go`
- Gọi `trace.InitTracer` khi khởi động ứng dụng và `defer tp.Shutdown` khi tắt.
- Tích hợp `trace.TracerMiddleware("notification-service")` vào Gin Engine của API.

#### 4. [MODIFY] `services/notification/internal/service/notification_service.go`
- Tích hợp span vào hàm `createFromTemplate`.
- Trong hàm `createWithOutbox`, gọi `trace.InjectMap` để lưu context vào payload của Outbox Event trước khi ghi vào Database.

#### 5. [MODIFY] `services/notification/internal/worker/outbox_worker.go`
- Giải mã `trace_context` từ outbox event payload, khôi phục context bằng `trace.ExtractMap`, tạo span `outbox.publish` trước khi publish tin nhắn lên RabbitMQ.

#### 6. [MODIFY] `services/notification/internal/worker/email_worker.go`
- Tạo span `worker.email.process` khi nhận tin.
- Tạo span `worker.email.send_smtp` khi kết nối gửi email sang Mailpit SMTP.

#### 7. [MODIFY] `services/notification/internal/worker/campaign_worker.go` & `webhook_worker.go`
- Tích hợp tracing đo lường latency của tiến trình gửi tin chiến dịch và xử lý webhook.

---

## 4. Cấu Hình Môi Trường Chạy

### A. Docker Compose (`docker-compose.yml`)
Bổ sung container Jaeger all-in-one:
```yaml
  jaeger:
    image: jaegertracing/all-in-one:1.57
    container_name: platform-jaeger
    ports:
      - "16686:16686"  # Jaeger UI (Trình duyệt)
      - "4317:4317"    # OTLP gRPC
      - "4318:4318"    # OTLP HTTP
    environment:
      - COLLECTOR_OTLP_ENABLED=true
```
Cấu hình biến môi trường cho `notification-service`:
- `OTEL_EXPORTER_OTLP_ENDPOINT=http://jaeger:4318`
- `OTEL_SERVICE_NAME=notification-service`

### B. Kubernetes (k8s)

#### 1. [NEW] `k8s/infrastructure/09_jaeger.yaml`
Khai báo Jaeger Deployment và Service (`jaeger-service` thuộc namespace `microservices-platform`). Expose các port tương ứng (`16686`, `4317`, `4318`).

#### 2. [MODIFY] `k8s/services/notification/09_notification.yaml`
Cấu hình ConfigMap chung:
```yaml
  OTEL_ENABLED: "true"
  OTEL_EXPORTER_OTLP_ENDPOINT: "http://jaeger-service:4318"
```
Đưa các biến môi trường này vào Deployment của `notification-api` và tất cả các Deployments của `workers` để kích hoạt tracing.

#### 3. [MODIFY] `k8s/tools/03_tools_ingress.yaml`
Cấu hình Ingress host-based routing để truy cập Jaeger UI trực quan qua domain local:
```yaml
    - host: jaeger.localhost
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: jaeger-service
                port:
                  number: 16686
```

---

## 5. Quy Trình Xác Minh & Kiểm Thử

### Bước 1: Khởi động hệ thống
- **Docker Compose:** `docker-compose up -d --build`
- **K8s:**
  ```bash
  kubectl apply -f k8s/infrastructure/09_jaeger.yaml
  kubectl apply -f k8s/services/notification/09_notification.yaml
  kubectl apply -f k8s/tools/03_tools_ingress.yaml
  ```

### Bước 2: Kích hoạt Trace
Gửi request gửi email thông báo:
```bash
curl -X POST http://localhost:8082/api/v1/notifications/send \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user-test-999",
    "event_type": "order_placed",
    "payload": {
      "recipient": "khachhang@example.com",
      "subject": "Xác nhận đơn hàng",
      "content": "Cảm ơn bạn đã mua hàng tại hệ thống của chúng tôi."
    }
  }'
```

### Bước 3: Kiểm tra Jaeger UI
- Docker: Mở `http://localhost:16686`
- K8s: Thêm `127.0.0.1 jaeger.localhost` vào file `hosts` và truy cập `http://jaeger.localhost`
- Tìm kiếm trace của service `notification-service`. Xác nhận chuỗi trace chứa đầy đủ các spans kế tiếp nhau tạo thành sơ đồ hình cây hoàn chỉnh, không bị đứt đoạn.
