# 🧪 Notification Service — Hướng Dẫn Test Manual

> **Mục tiêu:** Học kiến trúc, flow, và các công nghệ (RabbitMQ, Outbox, Circuit Breaker, ...) thông qua việc tự tay test và quan sát hệ thống phản ứng như thế nào.

---

## 📋 Mục Lục

1. [Khởi động môi trường](#1-khởi-động-môi-trường)
2. [Danh sách tool quan sát](#2-danh-sách-tool-quan-sát)
3. [Lab 1: Gửi Notification cơ bản](#3-lab-1-gửi-notification-cơ-bản)
4. [Lab 2: Outbox Pattern — Quan sát luồng async](#4-lab-2-outbox-pattern)
5. [Lab 3: Retry & Dead Letter Queue](#5-lab-3-retry--dlq)
6. [Lab 4: Circuit Breaker](#6-lab-4-circuit-breaker)
7. [Lab 5: User Preferences — Opt-out](#7-lab-5-user-preferences)
8. [Lab 6: Campaign Mass Notification](#8-lab-6-campaign-mass-notification)
9. [Lab 7: Scheduled Notification](#9-lab-7-scheduled-notification)
10. [Lab 8: DLQ Replay](#10-lab-8-dlq-replay)
11. [Lab 9: Scheduled Daily Notification](#11-lab-9-scheduled-daily-notification)
12. [Checklist học được gì](#12-checklist-học-được-gì)

---

## 1. Khởi Động Môi Trường

### Chạy trên Kubernetes (môi trường chính)

```bash
# Từ thư mục gốc project
make k8s-up          # Tạo cluster (lần đầu) hoặc resume

# Nếu cần deploy lại notification service sau khi sửa code:
make deploy-notification
```

### Kiểm tra pods đang chạy
```bash
kubectl get pods -n microservices-platform

# Kết quả mong đợi — CẦN thấy Running:
# notification-api-xxx              Running
# notification-worker-email-xxx     Running
# notification-worker-outbox-xxx    Running
# notification-worker-webhook-xxx   Running
```

### Kiểm tra logs của từng worker
```bash
# API logs
kubectl logs -f deployment/notification-api -n microservices-platform

# Outbox worker logs (quan trọng nhất để hiểu Outbox Pattern)
kubectl logs -f deployment/notification-worker-outbox -n microservices-platform

# Email worker logs
kubectl logs -f deployment/notification-worker-email -n microservices-platform
```

---

## 2. Danh Sách Tool Quan Sát

| Tool | URL | Dùng để xem gì |
|------|-----|----------------|
| **Swagger UI** | `http://localhost/notification/swagger/index.html` | Gọi API trực tiếp |
| **RabbitMQ Management** | `http://localhost:15672` (guest/guest) | Xem queue, message flow |
| **Mailpit** | `http://localhost:8025` | Xem email đã gửi |
| **Adminer/CloudBeaver** | `http://adminer.localhost` | Xem database tables trực tiếp |
| **Grafana** | `http://localhost/grafana` (admin/admin123) | Xem logs, metrics |

> 💡 **Mẹo:** Mở tất cả tab này song song khi test. Sau mỗi API call, refresh các tab để thấy sự thay đổi.

---

## 3. Lab 1: Gửi Notification Cơ Bản

**Mục tiêu học:** Hiểu flow từ API → Service → DB → Worker → RabbitMQ → Email

### Bước 1: Gọi API gửi notification

Dùng Swagger hoặc curl:

```bash
curl -X POST http://localhost/notification/api/v1/notifications/send \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user-001",
    "event_type": "auth_otp",
    "payload": {
      "recipient": "test@example.com",
      "otp": "123456",
      "name": "Nguyen Van A"
    }
  }'
```

**Kết quả mong đợi:**
```json
{
  "data": {
    "notification_id": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
    "message": "notification sent successfully"
  }
}
```

### Bước 2: Xem DB ngay sau khi gọi

Mở Adminer (`http://adminer.localhost`) → database `notification_db`:

```sql
-- Xem notification vừa tạo
SELECT id, event_type, status, recipient, created_at
FROM notifications
ORDER BY created_at DESC
LIMIT 5;

-- Xem outbox event tương ứng — ĐÂY LÀ CHÌA KHÓA của Outbox Pattern
SELECT id, aggregate_id, status, routing_key, retry_count, created_at
FROM outbox_events
ORDER BY created_at DESC
LIMIT 5;
```

> 🔍 **Quan sát:** Cả `notifications` và `outbox_events` đều có record **cùng lúc** (do cùng 1 DB transaction). Status ban đầu là `pending`.

### Bước 3: Đợi OutboxWorker poll (2 giây)

Sau 2 giây, chạy lại query:

```sql
-- Outbox event phải thay đổi status
SELECT id, status, published_at FROM outbox_events ORDER BY created_at DESC LIMIT 5;
```

> 🔍 **Quan sát:** `status` đổi từ `pending` → `processing` → `published`. `published_at` được set.

### Bước 4: Xem RabbitMQ

Mở `http://localhost:15672` → Queues → tìm queue `email`:
- Xem **Messages** counter
- Click vào queue → **Get messages** để xem nội dung message

> 🔍 **Quan sát:** Message JSON chứa `notification_id`, `recipient`, `subject`, `content`, `trace_context`.

### Bước 5: Xem email đã gửi

Mở Mailpit `http://localhost:8025` → xem email `test@example.com` vừa nhận được.

### Bước 6: Xem DB sau khi email worker xử lý

```sql
-- Status notification phải đổi sang 'delivering' (chờ webhook confirm)
SELECT id, status, retry_count, sent_at FROM notifications ORDER BY created_at DESC LIMIT 5;
```

### ✅ Tóm tắt Flow bạn vừa thấy

```
POST /send
  → INSERT notifications (pending) + INSERT outbox_events (pending) — 1 transaction
  → OutboxWorker poll (2s) → RabbitMQ queue 'email'
  → EmailWorker SMTP send → Mailpit nhận email
  → Mailpit webhook → status = 'delivering'
```

---

## 4. Lab 2: Outbox Pattern

**Mục tiêu học:** Tại sao cần Outbox? Atomic write, không mất message.

### Xem payload outbox chứa trace context

```sql
SELECT
  id,
  aggregate_id,
  event_type,
  routing_key,
  status,
  payload::text
FROM outbox_events
WHERE status = 'published'
ORDER BY created_at DESC
LIMIT 1;
```

> 🔍 **Quan sát trong payload:**
> - `notification_id` — để EmailWorker biết update status record nào
> - `event_id` — dùng để **deduplication** (nếu worker crash và publish lại, consumer bỏ qua)
> - `trace_context` — W3C traceparent, cho phép trace xuyên qua async boundary

### Xem thống kê trạng thái outbox

```sql
SELECT status, COUNT(*) FROM outbox_events GROUP BY status;
```

> 🔍 **Nếu `pending` tăng liên tục** → OutboxWorker đang có vấn đề. Kiểm tra logs ngay.

---

## 5. Lab 3: Retry & DLQ

**Mục tiêu học:** Retry strategy, Dead Letter Queue hoạt động như thế nào.

### Xem DLQ messages qua API

```bash
curl "http://localhost/notification/admin/dlq/messages?status=pending&page=1&limit=10"
```

### Xem trong DB

```sql
-- Notifications đã fail sau max retry
SELECT id, event_type, status, retry_count, error_message
FROM notifications
WHERE status = 'failed'
ORDER BY created_at DESC;

-- DLQ messages
SELECT id, notification_id, queue_name, error_message, status, created_at
FROM dlq_messages
ORDER BY created_at DESC;
```

### Xem retry queue trong RabbitMQ

Mở RabbitMQ UI → Queues → tìm `email.retry`:
- Queue này dùng **TTL** (1 phút / 5 phút / 15 phút)
- Sau TTL, message tự động quay lại queue `email` qua Dead Letter Exchange

> 🔍 **Pattern retry:**
> ```
> email queue → FAIL → email.retry (TTL 60s) → email queue
>             → FAIL → email.retry (TTL 5m)  → email queue
>             → FAIL → email.retry (TTL 15m) → email queue
>             → FAIL (max) → email.dlq → DLQWorker → dlq_messages table
> ```

---

## 6. Lab 4: Circuit Breaker

**Mục tiêu học:** Circuit Breaker bảo vệ hệ thống khi SMTP down.

### Xem Circuit Breaker trong logs

```bash
kubectl logs -f deployment/notification-worker-email -n microservices-platform | grep -i "circuit"
```

### Hiểu 3 trạng thái qua log messages

| Log message | Trạng thái | Ý nghĩa |
|-------------|-----------|---------|
| `"Sending email"` | CLOSED | Bình thường, thực thi SMTP |
| `"Circuit breaker is open, skipping"` | OPEN | SMTP đang down, fast-fail |
| *(không log, chỉ thử 1 request)* | HALF_OPEN | Đang probe để kiểm tra |

### State machine (đọc file để hiểu rõ hơn)

Mở `services/notification/internal/worker/circuit_breaker.go`:

```
CLOSED → (failureCount >= maxFailures=5) → OPEN
OPEN   → (sau openDuration=30s)          → HALF_OPEN
HALF_OPEN → SUCCESS → CLOSED (reset failureCount=0)
HALF_OPEN → FAIL    → OPEN (trở lại)
```

> 💡 **Bài học:** Fast-fail khi SMTP down → NACK message → requeue → thử lại sau. Tránh blocking toàn bộ worker thread.

---

## 7. Lab 5: User Preferences — Opt-out

**Mục tiêu học:** User có thể tắt notification, service phải tôn trọng.

### Bước 1: Tắt notification cho user

```bash
curl -X PUT http://localhost/notification/api/v1/users/user-001/notification-preferences \
  -H "Content-Type: application/json" \
  -d '{
    "preferences": [
      {
        "event_type": "promotion_campaign",
        "channel": "email",
        "enabled": false
      }
    ]
  }'
```

### Bước 2: Gửi notification promotion cho user đó

```bash
curl -X POST http://localhost/notification/api/v1/notifications/send \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user-001",
    "event_type": "promotion_campaign",
    "payload": {
      "recipient": "test@example.com"
    }
  }'
```

**Kết quả mong đợi:**
```json
{
  "data": {
    "message": "notification skipped - user opted out"
  }
}
```

> 🔍 **Quan sát:** DB **không có** record mới. Mailpit **không nhận** email. Pre-flight check ở Service Layer — trước khi tạo DB record.

### Bước 3: Xem preferences trong DB

```sql
SELECT user_id, event_type, channel, enabled, updated_at
FROM notification_preferences
WHERE user_id = 'user-001';
```

---

## 8. Lab 6: Campaign Mass Notification

**Mục tiêu học:** Dispatch hàng loạt theo batch, checkpoint-resume.

### Bước 1: Tạo campaign

```bash
curl -X POST http://localhost/notification/admin/campaigns \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Chào mừng tháng 5",
    "subject": "Ưu đãi đặc biệt tháng 5",
    "content": "Xin chào, bạn có ưu đãi mới!",
    "channel": "email",
    "event_type": "promotion_campaign",
    "scheduled_at": "2026-05-18T15:00:00Z",
    "target_audience": "specific",
    "recipients": [
      {"user_id": "user-001", "recipient": "user1@example.com"},
      {"user_id": "user-002", "recipient": "user2@example.com"},
      {"user_id": "user-003", "recipient": "user3@example.com"}
    ]
  }'
```

> **Lưu ý:** `scheduled_at` phải là thời gian **trong tương lai**.

### Bước 2: Xem campaign trong DB

```sql
SELECT id, title, status, total_recipients, dispatched_count, sent_count, failed_count, last_dispatched_offset
FROM campaigns
ORDER BY created_at DESC LIMIT 5;

-- Xem recipients
SELECT campaign_id, user_id, recipient, status
FROM campaign_recipients
ORDER BY created_at DESC;
```

### Bước 3: Theo dõi dispatch (khi đến scheduled_at)

```bash
# CampaignWorker tick mỗi 10 giây
kubectl logs -f deployment/notification-worker-email -n microservices-platform | grep -i "campaign"
```

### Bước 4: Xem tiến trình qua API

```bash
curl http://localhost/notification/admin/campaigns/{campaign_id}/stats
```

```json
{
  "status": "dispatching",
  "total_recipients": 3,
  "dispatched_count": 3,
  "sent_count": 2,
  "failed_count": 0,
  "pending_count": 1,
  "progress_pct": 66.67
}
```

> 🔍 **Khái niệm học được:**
> - **Checkpoint:** `last_dispatched_offset` — nếu worker crash, restart tiếp tục từ đây
> - **Back-pressure:** Queue có `x-max-length=50000` — nếu đầy, worker backoff exponential
> - **Audit:** Mỗi email campaign gửi xong → INSERT 1 record vào `notifications` table

---

## 9. Lab 7: Scheduled Notification

**Mục tiêu học:** Gửi notification theo lịch, SchedulerWorker.

### Tạo notification scheduled trong tương lai gần

```bash
curl -X POST http://localhost/notification/api/v1/notifications/schedule \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user-001",
    "event_type": "auth_otp",
    "scheduled_at": "2026-05-18T22:05:00+07:00",
    "payload": {
      "recipient": "test@example.com",
      "otp": "999888",
      "name": "Test User"
    }
  }'
```

### Xem trong DB

```sql
-- Notification được tạo nhưng scheduled_at ở tương lai
SELECT id, status, scheduled_at, created_at
FROM notifications
WHERE event_type = 'auth_otp'
ORDER BY created_at DESC LIMIT 3;
```

### Validation rules (thử cố tình vi phạm để xem lỗi)

```bash
# Lỗi 1: scheduled_at ở quá khứ
"scheduled_at": "2020-01-01T00:00:00Z"
# → lỗi: "scheduled_at must be in the future"

# Lỗi 2: scheduled_at quá xa (hơn 1 năm)
"scheduled_at": "2030-01-01T00:00:00Z"
# → lỗi: "scheduled_at cannot be more than 1 year in the future"
```

---

## 10. Lab 8: DLQ Replay

**Mục tiêu học:** Xử lý message thất bại, replay về main queue.

### Bước 1: Xem DLQ messages

```bash
curl "http://localhost/notification/admin/dlq/messages?status=pending&page=1&limit=10"
```

### Bước 2: Replay 1 message

```bash
# Lấy id từ kết quả trên
curl -X POST http://localhost/notification/admin/dlq/replay/{dlq_message_id}
```

### Bước 3: Xem kết quả trong DB

```sql
-- DLQ message phải đổi status = 'replayed'
SELECT id, status FROM dlq_messages WHERE id = '{dlq_message_id}';

-- Notification gốc phải đổi status = 'pending' (retry từ đầu)
SELECT id, status, retry_count FROM notifications WHERE id = '{notification_id}';
```

> 🔍 **Logic quan trọng:** Khi replay, `retry_count` được **reset về 0** → notification có cơ hội được gửi lại từ đầu.

---

## 11. Lab 9: Scheduled Daily Notification

**Mục tiêu học:** User tự cài lịch nhận reminder hàng ngày.

### Tạo schedule

```bash
curl -X PUT http://localhost/notification/api/v1/users/user-001/notification-schedules \
  -H "Content-Type: application/json" \
  -d '{
    "event_type": "daily_reminder",
    "send_time": "08:00",
    "timezone": "Asia/Ho_Chi_Minh",
    "enabled": true,
    "payload": {
      "recipient": "test@example.com",
      "name": "Nguyen Van A"
    }
  }'
```

### Xem schedules

```bash
curl http://localhost/notification/api/v1/users/user-001/notification-schedules
```

```sql
SELECT user_id, event_type, send_time, timezone, enabled, last_sent_at
FROM user_notification_schedules;
```

### Xóa schedule

```bash
curl -X DELETE http://localhost/notification/api/v1/users/user-001/notification-schedules \
  -H "Content-Type: application/json" \
  -d '{"event_type": "daily_reminder"}'
```

---

## 12. Checklist Học Được Gì

### Về Outbox Pattern
- [ ] Tại sao cần Outbox? (dual-write problem — DB và MQ không thể atomic)
- [ ] 1 transaction ghi cả `notifications` lẫn `outbox_events`
- [ ] `FOR UPDATE SKIP LOCKED` đảm bảo multi-pod không duplicate publish
- [ ] At-least-once: nếu worker crash sau publish nhưng trước mark published → duplicate → dedup

### Về RabbitMQ
- [ ] Exchange → routing_key → Queue
- [ ] Dead Letter Exchange (DLX): `email.dlq`, `email.retry`
- [ ] TTL message trong retry queue (1min → 5min → 15min)
- [ ] `x-max-length` + `reject-publish` cho back-pressure ở campaign queue

### Về Circuit Breaker
- [ ] 3 states: CLOSED / OPEN / HALF_OPEN
- [ ] Bảo vệ SMTP server không bị overload khi down
- [ ] Fast-fail thay vì đợi timeout → tiết kiệm resource

### Về Campaign
- [ ] Batch dispatch với checkpoint-resume
- [ ] `last_dispatched_offset` = vị trí cuối đã dispatch → safe restart
- [ ] Audit record: mỗi email campaign → 1 notification record

### Về Design
- [ ] `APP_MODE` env var quyết định process là API hay Worker nào
- [ ] Cùng 1 binary, chạy khác nhau tùy mode
- [ ] Template engine: `{{.name}}` → render trước khi gửi

---

## 🛠️ Câu Lệnh Hay Dùng

```bash
# Xem tất cả pods
kubectl get pods -n microservices-platform

# Xem logs real-time
kubectl logs -f <pod-name> -n microservices-platform

# Xem events debug crash
kubectl get events -n microservices-platform --sort-by='.lastTimestamp'

# Restart deployment
kubectl rollout restart deployment/notification-worker-outbox -n microservices-platform

# Scale worker (test multi-pod, quan sát SKIP LOCKED)
kubectl scale deployment/notification-worker-outbox --replicas=3 -n microservices-platform
```

```sql
-- Thống kê notification theo status
SELECT status, COUNT(*) FROM notifications GROUP BY status;

-- Outbox backlog (tăng liên tục → worker có vấn đề)
SELECT COUNT(*) FROM outbox_events WHERE status = 'pending';

-- DLQ backlog
SELECT COUNT(*) FROM dlq_messages WHERE status = 'pending';

-- Notifications gần nhất
SELECT id, event_type, channel, status, retry_count, created_at
FROM notifications
ORDER BY created_at DESC LIMIT 10;
```
