# Notification Service — Feature Roadmap

Các tính năng có thể build thêm để học chuyên sâu về backend, sắp xếp theo thứ tự học được khuyến nghị.

---

## 1. Notification History API — Pagination & Filtering (DONE)

**Chủ đề học:** Cursor-based pagination, composite index, API design

Hiện tại không có API nào để query lịch sử notification. Cần build:

- `GET /api/v1/notifications?user_id=&status=&channel=&event_type=&from=&to=`
- Cursor pagination dùng `(created_at, id)` thay vì `OFFSET` để tránh slow query trên bảng lớn
- So sánh `EXPLAIN ANALYZE` giữa cursor vs offset để thấy rõ sự khác biệt
- Thêm composite index `(user_id, created_at DESC, id)` để tối ưu

**Use-case thực tế:**
> App thương mại điện tử như Shopee — user vào mục "Thông báo" muốn xem lại lịch sử: đơn hàng nào đã giao, khuyến mãi nào đã nhận. Bảng có hàng chục triệu row, nếu dùng `OFFSET` thì trang 500 sẽ mất vài giây; cursor pagination giữ latency ổn định ở trang bất kỳ.

---

## 2. Scheduled Notifications — Delayed Delivery (DONE)

**Chủ đề học:** Background job scheduling, time-based query, cron patterns trong Go

Thêm khả năng đặt lịch gửi notification vào tương lai:

- Thêm column `scheduled_at TIMESTAMPTZ` vào bảng `notifications`
- `worker-pending` chỉ claim các row có `scheduled_at <= NOW()`
- API: `POST /api/v1/notifications/schedule` với field `scheduled_at`

**Use-case thực tế:**
> Nền tảng học online như Duolingo — sau khi user đăng ký, hệ thống tự động đặt lịch:
> - +1 giờ: email "Bắt đầu bài học đầu tiên của bạn"
> - +24 giờ: push notification "Đừng bỏ lỡ streak ngày 2!"
> - +7 ngày: email "Mẹo học nhanh hơn cho người mới"
>
> Tất cả được insert một lần ngay lúc đăng ký, worker tự gửi đúng giờ mà không cần logic phức tạp.

---

## 3. Mass Notification Campaign — Fan-out Delivery (DONE)

**Chủ đề học:** Fan-out pattern, batch processing, back-pressure, checkpointing

Gửi một thông báo đến toàn bộ (hoặc một nhóm lớn) user vào một thời điểm định sẵn — ví dụ: thông báo ra mắt tính năng mới lúc 12h trưa đến hàng triệu người dùng.

**Tại sao không dùng Scheduled Notification (feature 2)?**
Feature 2 là 1 notification → 1 user. Feature này là 1 campaign → N triệu users. INSERT N triệu rows cùng lúc sẽ làm chết DB và flood queue.

**Kiến trúc đúng:**

```
Admin tạo Campaign { message, target_group, scheduled_at: 12:00 }
         │
    campaigns table (1 row duy nhất)
         │
    12:00 đến
         │
         ▼
  campaign-dispatcher (worker mới, APP_MODE=worker-campaign)
    - Đọc danh sách user theo batch (1.000 user/lần)
    - Publish job vào RabbitMQ — KHÔNG insert notifications table trước
         │
         ▼
  worker-email (nhiều pod song song)
    - Consume từ queue, gửi SMTP
    - Insert notification record SAU KHI gửi xong (audit log)
```

**Các thành phần cần thêm:**

- Bảng `campaigns (id, title, message, channel, target_group, scheduled_at, status, last_dispatched_offset)`
- `worker-campaign`: đọc user theo batch, publish job, checkpoint `last_dispatched_offset` để resume nếu crash
- API:
  - `POST /admin/campaigns` — tạo campaign mới
  - `GET /admin/campaigns/:id/stats` — theo dõi tiến độ gửi

**Các pattern quan trọng:**

| Pattern | Mục đích |
|---------|---------|
| Batch fan-out | Không load toàn bộ user vào RAM, xử lý 1.000/lần |
| Back-pressure | RabbitMQ prefetch limit — producer không publish nhanh hơn consumer xử lý được |
| Checkpointing | Lưu `last_dispatched_offset` vào DB để dispatcher resume sau khi crash, tránh gửi duplicate |
| Audit log sau gửi | Không tốn DB write trước khi biết có gửi được không |

**Use-case thực tế:**
> Một SaaS platform ra mắt tính năng AI mới — đúng 12h trưa thứ Hai, toàn bộ 2 triệu user nhận email "Khám phá tính năng mới". Nếu insert 2 triệu row cùng lúc → DB timeout. Với fan-out pattern, dispatcher xử lý 1.000 user/batch trong ~33 phút, 20 pod worker-email chạy song song, SMTP không bị rate-limit, hệ thống vẫn phục vụ traffic bình thường.

---

## 4. Prometheus Metrics + Grafana Dashboard

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

**Use-case thực tế:**
> Đêm Black Friday, đội on-call mở Grafana thấy `notifications_failed_total{reason="smtp_timeout"}` tăng đột biến — phát hiện ngay SMTP provider đang quá tải trước khi user báo cáo. Không có metrics, đội chỉ biết có vấn đề khi support ticket tràn vào.

---

## 5. Circuit Breaker cho SMTP — Resilience Pattern (DONE)

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

**Use-case thực tế:**
> SendGrid (SMTP provider) bị sự cố 30 phút. Không có circuit breaker: 50.000 email cứ retry liên tục, RabbitMQ queue tắc nghẽn, memory worker tăng vọt, cả pipeline bị ảnh hưởng kể cả webhook. Với circuit breaker: sau 5 lần fail liên tiếp, circuit OPEN — worker dừng retry ngay, queue không tắc, webhook vẫn hoạt động bình thường. Sau 30 phút, HALF-OPEN thử lại và tự recover.

---

## 6. Dead Letter Queue (DLQ) — RabbitMQ nâng cao (DONE)

**Chủ đề học:** DLQ pattern, message TTL, replay mechanism, tách retry logic khỏi application

Hiện tại retry được xử lý ở tầng application (ghi lại DB, worker-pending pick up lại). Thay bằng DLQ ở broker level:

- Khai báo `notification.dlq` exchange + `dlq.email` queue
- Bind DLQ vào main queue với `x-dead-letter-exchange`
- Khi message NACK sau max retries → RabbitMQ tự route sang DLQ
- Thêm `worker-dlq` mode để inspect và replay thủ công
- API: `POST /admin/dlq/replay/:message_id`

**Use-case thực tế:**
> Email chứa link reset password của user bị fail vì template render lỗi (bug code). Message vào DLQ, developer fix bug, deploy xong → replay toàn bộ DLQ: user nhận được email đúng giờ mà không cần user yêu cầu lại. Nếu không có DLQ, những message này mất vĩnh viễn.

---

## 7. Outbox Pattern — Guaranteed Delivery (DONE)

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

**Use-case thực tế:**
> Hệ thống banking — user chuyển tiền thành công, service ghi DB xong nhưng pod crash trước khi publish RabbitMQ. Không có Outbox: user không nhận được SMS xác nhận, gọi hotline hỏi "tiền có chuyển chưa?". Với Outbox: outbox worker tự recover và đảm bảo SMS luôn được gửi sau khi transaction committed, dù pod có crash bất cứ lúc nào.

---

## 8. User Notification Preferences — Feature Design (DONE)

**Chủ đề học:** Schema design, business logic phức tạp, user-facing API

Cho phép user tự quản lý notification họ muốn nhận:

- Bảng `notification_preferences (user_id, event_type, channel, enabled)`
- `CreateNotification` check preference trước khi tạo record
- API:
  - `GET /api/v1/users/:id/notification-preferences`
  - `PUT /api/v1/users/:id/notification-preferences`
- Cache preferences trong Redis (TTL 5 phút) để tránh query DB mỗi request

**Use-case thực tế:**
> User LinkedIn chỉ muốn nhận email về "job alert" và "connection request", tắt hết notification marketing. Mỗi khi có event, hệ thống check preferences trong Redis (cache hit ~99%) → bỏ qua nếu user đã tắt channel đó. Không có feature này, user spam unsubscribe hoặc block email domain — ảnh hưởng deliverability toàn hệ thống.

---

## 9. OpenTelemetry Distributed Tracing

**Chủ đề học:** Distributed tracing, trace propagation qua message broker, Jaeger/Tempo

Trace một notification từ lúc POST `/send` đến khi email được confirm delivered — xuyên qua API → RabbitMQ → Worker → SMTP → Webhook:

- Inject `traceparent` header vào RabbitMQ message khi publish
- Extract ở worker side để tạo child span (trace liên tục qua broker)
- Export sang Jaeger (có thể deploy vào K8s cluster sẵn có)
- Visualize full journey: thấy rõ bottleneck ở bước nào

**Use-case thực tế:**
> User báo cáo "tôi đặt lịch notification 9h sáng nhưng đến 9h15 mới nhận được". Không có tracing: debug mù, không biết delay ở bước nào (API? Queue? Worker? SMTP?). Với OpenTelemetry: mở Jaeger, lọc theo `user_id`, thấy ngay span `worker-email.send_smtp` mất 14 phút vì SMTP connection pool bị saturated — fix bằng cách tăng pool size.

---

## Thứ tự học được khuyến nghị

```
[1]  History API          → nền tảng, dễ thấy kết quả ngay [DONE]
[2]  Scheduled Noti       → mở rộng worker hiện có, ít thay đổi [DONE]
[3]  Mass Campaign        → fan-out pattern, batch processing thực chiến [DONE]
[4]  Prometheus Metrics   → thấy hệ thống "sống", cần cho monitoring [DONE]
[5]  Circuit Breaker      → resilience pattern quan trọng nhất [DONE]
[6]  Dead Letter Queue    → RabbitMQ nâng cao, broker-level retry [DONE]
[7]  Outbox Pattern       → distributed systems concept cốt lõi [DONE]
[8]  Preferences API      → schema design + cache layer [DONE]
[9]  OpenTelemetry        → khó nhất, reward cao nhất về observability
```

---

## Ghi chú

- Mỗi tính năng nên có unit test cho service layer và integration test cho repository layer
- Dùng `EXPLAIN ANALYZE` để verify query performance trước khi merge
- Mọi config mới đều qua env var (không hardcode), thêm vào `.env.example`
