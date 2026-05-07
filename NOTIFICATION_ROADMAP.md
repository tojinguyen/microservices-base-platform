# Notification Service — Feature Roadmap

Các tính năng có thể build thêm để học chuyên sâu về backend, sắp xếp theo thứ tự học được khuyến nghị.

---

## 1. Notification History API — Pagination & Filtering

**Chủ đề học:** Cursor-based pagination, composite index, API design

Hiện tại không có API nào để query lịch sử notification. Cần build:

- `GET /api/v1/notifications?user_id=&status=&channel=&event_type=&from=&to=`
- Cursor pagination dùng `(created_at, id)` thay vì `OFFSET` để tránh slow query trên bảng lớn
- So sánh `EXPLAIN ANALYZE` giữa cursor vs offset để thấy rõ sự khác biệt
- Thêm composite index `(user_id, created_at DESC, id)` để tối ưu

---

## 2. Scheduled Notifications — Delayed Delivery

**Chủ đề học:** Background job scheduling, time-based query, cron patterns trong Go

Thêm khả năng đặt lịch gửi notification vào tương lai:

- Thêm column `scheduled_at TIMESTAMPTZ` vào bảng `notifications`
- `worker-pending` chỉ claim các row có `scheduled_at <= NOW()`
- API: `POST /api/v1/notifications/schedule` với field `scheduled_at`
- Use case: reminder sau 24h, campaign giờ cao điểm, welcome email sau onboarding

---

## 3. Prometheus Metrics + Grafana Dashboard

**Chủ đề học:** Observability, instrumentation, RED method (Rate / Errors / Duration)

Identity service đã có `/metrics`, notification service chưa có. Cần thêm:

| Metric | Type | Labels |
|--------|------|--------|
| `notifications_sent_total` | Counter | `channel`, `event_type` |
| `notifications_failed_total` | Counter | `channel`, `reason` |
| `notification_delivery_duration_seconds` | Histogram | `channel` |
| `rabbitmq_queue_depth` | Gauge | `queue_name` |
| `worker_batch_size` | Histogram | `worker` |

Sau đó build Grafana dashboard từ các metrics này và deploy vào K8s cluster.

---

## 4. Circuit Breaker cho SMTP — Resilience Pattern

**Chủ đề học:** Circuit breaker pattern (Closed → Open → Half-Open), tránh cascade failure

Vấn đề hiện tại: nếu SMTP provider down 1 tiếng, mọi message đều bị retry vô ích và làm tắc queue.

- Implement circuit breaker bao quanh SMTP call trong `email_worker.go`
- Dùng `sony/gobreaker` hoặc tự viết state machine
- Khi circuit OPEN: skip send, NACK message về queue, log trạng thái
- Expose circuit state qua `/metrics` hoặc `/health`

**3 trạng thái cần implement:**
```
CLOSED  → request bình thường, đếm failure
OPEN    → block tất cả request, trả lỗi ngay
HALF-OPEN → cho qua 1 request thử, nếu OK thì về CLOSED
```

---

## 5. Dead Letter Queue (DLQ) — RabbitMQ nâng cao

**Chủ đề học:** DLQ pattern, message TTL, replay mechanism, tách retry logic khỏi application

Hiện tại retry được xử lý ở tầng application (ghi lại DB, worker-pending pick up lại). Thay bằng DLQ ở broker level:

- Khai báo `notification.dlq` exchange + `dlq.email` queue
- Bind DLQ vào main queue với `x-dead-letter-exchange`
- Khi message NACK sau max retries → RabbitMQ tự route sang DLQ
- Thêm `worker-dlq` mode để inspect và replay thủ công
- API: `POST /admin/dlq/replay/:message_id`

---

## 6. Outbox Pattern — Guaranteed Delivery

**Chủ đề học:** Distributed systems, dual-write problem, at-least-once delivery

**Vấn đề cần giải quyết:** nếu service crash sau khi ghi DB nhưng trước khi publish RabbitMQ, notification bị mất.

```
Hiện tại:  ghi DB → (crash ở đây?) → publish RabbitMQ
Outbox:    ghi DB + outbox trong 1 transaction → outbox worker poll + publish
```

- Thêm bảng `outbox_events (id, payload, published_at, created_at)`
- `CreateNotification` ghi notification + outbox record trong **cùng 1 DB transaction**
- Outbox worker poll `published_at IS NULL`, publish rồi cập nhật `published_at`
- Đảm bảo at-least-once delivery, kết hợp với `event_id` deduplication đã có

---

## 7. User Notification Preferences — Feature Design

**Chủ đề học:** Schema design, business logic phức tạp, user-facing API

Cho phép user tự quản lý notification họ muốn nhận:

- Bảng `notification_preferences (user_id, event_type, channel, enabled)`
- `CreateNotification` check preference trước khi tạo record
- API:
  - `GET /api/v1/users/:id/notification-preferences`
  - `PUT /api/v1/users/:id/notification-preferences`
- Cache preferences trong Redis (TTL 5 phút) để tránh query DB mỗi request

---

## 8. Digest / Batching — Advanced Delivery

**Chủ đề học:** Message aggregation, time-window batching, trade-off latency vs UX

Thay vì gửi 10 email riêng lẻ cho cùng 1 user trong 1 giờ, gộp thành 1 email:

- Thêm column `digest_key VARCHAR` = `{user_id}:{date}:{channel}`
- Thêm column `digest_sent_at TIMESTAMPTZ`
- Digest worker chạy định kỳ (ví dụ: mỗi giờ), query và group theo `digest_key`
- Template riêng cho digest (list các notification trong ngày)

---

## 9. Multi-tenant API Key Auth — Security Layer

**Chủ đề học:** API key management, per-tenant rate limiting, authorization middleware

Hiện tại `/send` không cần auth — bất kỳ ai cũng gọi được.

- Bảng `api_keys (id, tenant_id, hashed_key, quota_per_day, last_used_at)`
- Middleware: lookup Redis cache trước (key = hash), fallback DB
- Rate limit per API key thay vì per IP
- API:
  - `POST /admin/api-keys` — tạo key mới
  - `DELETE /admin/api-keys/:id` — revoke
  - `GET /admin/api-keys/:id/usage` — usage stats

---

## 10. OpenTelemetry Distributed Tracing

**Chủ đề học:** Distributed tracing, trace propagation qua message broker, Jaeger/Tempo

Trace một notification từ lúc POST `/send` đến khi email được confirm delivered — xuyên qua API → RabbitMQ → Worker → SMTP → Webhook:

- Inject `traceparent` header vào RabbitMQ message khi publish
- Extract ở worker side để tạo child span (trace liên tục qua broker)
- Export sang Jaeger (có thể deploy vào K8s cluster sẵn có)
- Visualize full journey: thấy rõ bottleneck ở bước nào

---

## Thứ tự học được khuyến nghị

```
[1] History API          → nền tảng, dễ thấy kết quả ngay
[2] Scheduled Noti       → mở rộng worker hiện có, ít thay đổi
[3] Prometheus Metrics   → thấy hệ thống "sống", cần cho monitoring
[4] Circuit Breaker      → resilience pattern quan trọng nhất
[5] Dead Letter Queue    → RabbitMQ nâng cao, broker-level retry
[6] Outbox Pattern       → distributed systems concept cốt lõi
[7] Preferences API      → schema design + cache layer
[8] Digest/Batching      → advanced delivery logic
[9] API Key Auth         → security layer hoàn chỉnh
[10] OpenTelemetry       → khó nhất, reward cao nhất về observability
```

---

## Ghi chú

- Mỗi tính năng nên có unit test cho service layer và integration test cho repository layer
- Dùng `EXPLAIN ANALYZE` để verify query performance trước khi merge
- Mọi config mới đều qua env var (không hardcode), thêm vào `.env.example`
