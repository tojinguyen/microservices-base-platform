# Outbox Pattern — Guaranteed Delivery cho Notification Service

## Mục tiêu học

Sau khi đọc và implement tài liệu này, bạn sẽ hiểu:

- Tại sao dual-write problem xảy ra và tại sao nó nguy hiểm
- Outbox Pattern là gì và cách nó giải quyết vấn đề
- Cách implement outbox polling worker an toàn với multi-pod
- Sự khác biệt giữa polling approach và CDC (Change Data Capture)
- At-least-once delivery và cách kết hợp với deduplication

---

## 1. Vấn đề: Dual-Write Problem

### Kiến trúc hiện tại

```
POST /send
    │
    ▼
CreateNotification()
    │
    ├── INSERT INTO notifications (...)   ← Bước 1: ghi DB
    │
    └── broker.Publish(...)               ← Bước 2: publish RabbitMQ
```

### Kịch bản crash

```
INSERT INTO notifications → COMMIT ✓
                          ↓
                     POD CRASH 💥
                          ↓
broker.Publish() → KHÔNG BAO GIỜ CHẠY

Kết quả: DB có record, RabbitMQ không có message → notification bị mất vĩnh viễn
```

Đây là **dual-write problem**: bạn cần ghi vào 2 hệ thống khác nhau (PostgreSQL và RabbitMQ) một cách atomic, nhưng không có distributed transaction nào bao phủ cả hai.

### Tại sao không dùng distributed transaction (2PC)?

Two-Phase Commit (2PC) tồn tại nhưng có vấn đề nghiêm trọng:

| Vấn đề | Chi tiết |
|--------|---------|
| **Performance** | Mỗi write phải lock resource trên cả 2 hệ thống cho đến khi cả 2 confirm |
| **Availability** | Nếu coordinator crash giữa chừng, resource bị lock vô thời hạn |
| **RabbitMQ không support 2PC** | AMQP protocol không có distributed transaction semantics |

→ **Outbox Pattern là giải pháp thực tế được dùng trong production.**

---

## 2. Outbox Pattern là gì?

### Ý tưởng cốt lõi

Thay vì ghi DB rồi publish MQ (2 hành động riêng lẻ), bạn:

1. Ghi notification record **VÀ** outbox record trong **cùng 1 DB transaction**
2. Một **outbox worker** riêng biệt poll outbox table và publish lên MQ

```
CreateNotification() — 1 transaction duy nhất:
    ├── INSERT INTO notifications (...)
    └── INSERT INTO outbox_events (...)
         COMMIT ← atomic, hoặc cả 2 thành công hoặc cả 2 rollback

Outbox Worker (process riêng):
    poll outbox_events WHERE published_at IS NULL
         │
         ├── broker.Publish(payload)
         │
         └── UPDATE outbox_events SET published_at = NOW()
```

### Tại sao điều này an toàn?

- Nếu pod crash **trước** khi commit transaction → cả 2 record bị rollback → không có gì trong DB, worker không có gì để xử lý → **OK, notification chưa được tạo**
- Nếu pod crash **sau** khi commit transaction → outbox record tồn tại trong DB → worker sẽ pick up và publish sau → **notification được gửi, chỉ là trễ hơn một chút**
- Nếu worker crash **sau** khi publish nhưng **trước** khi update `published_at` → worker restart, re-publish → **duplicate**, cần deduplication ở consumer side

---

## 3. Schema Design

### Bảng `outbox_events`

```sql
-- +goose Up
CREATE TABLE outbox_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate_id    UUID NOT NULL,              -- notification_id để trace
    aggregate_type  TEXT NOT NULL,              -- 'notification' (mở rộng sau)
    event_type      TEXT NOT NULL,              -- 'notification.created', 'notification.retry'
    payload         JSONB NOT NULL,             -- toàn bộ message publish lên MQ
    routing_key     TEXT NOT NULL,              -- RabbitMQ routing key, vd: 'email', 'webhook'
    status          TEXT NOT NULL DEFAULT 'pending', -- pending | published | failed
    retry_count     INT NOT NULL DEFAULT 0,
    last_error      TEXT,                       -- lỗi cuối cùng nếu có
    published_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index để worker query hiệu quả
CREATE INDEX idx_outbox_events_pending
    ON outbox_events (created_at ASC)
    WHERE published_at IS NULL AND status = 'pending';

-- Index để trace theo notification
CREATE INDEX idx_outbox_events_aggregate
    ON outbox_events (aggregate_id, aggregate_type);

-- +goose Down
DROP TABLE outbox_events;
```

### Lý do từng field

| Field | Lý do |
|-------|-------|
| `aggregate_id` | Cho phép trace "outbox event này thuộc notification nào", debug dễ hơn |
| `aggregate_type` | Thiết kế mở: sau này có thể dùng outbox cho domain khác (campaigns, alerts) |
| `event_type` | Worker biết phải xử lý event này như thế nào, có thể route khác nhau |
| `routing_key` | Lưu sẵn routing key → worker không cần đọc thêm DB để biết publish đâu |
| `status` | Phân biệt `pending` vs `failed` (đã retry quá nhiều) vs `published` |
| `retry_count` + `last_error` | Audit trail, tránh retry vô hạn |
| `published_at` | Dùng cho partial index — worker chỉ scan row chưa publish |

### Bảng `notifications` — không thay đổi interface

Không cần thay đổi schema `notifications`. Outbox chỉ thêm vào bên trong transaction của `CreateNotification`.

---

## 4. Implementation

### 4.1 Repository Layer

```go
// internal/repository/outbox_repository.go

type OutboxEvent struct {
    ID            uuid.UUID  `gorm:"primaryKey;type:uuid"`
    AggregateID   uuid.UUID  `gorm:"not null;type:uuid"`
    AggregateType string     `gorm:"not null"`
    EventType     string     `gorm:"not null"`
    Payload       []byte     `gorm:"type:jsonb;not null"`
    RoutingKey    string     `gorm:"not null"`
    Status        string     `gorm:"not null;default:pending"`
    RetryCount    int        `gorm:"not null;default:0"`
    LastError     *string
    PublishedAt   *time.Time
    CreatedAt     time.Time  `gorm:"not null"`
}

type OutboxRepository interface {
    CreateWithTx(tx *gorm.DB, event *OutboxEvent) error
    FetchPending(ctx context.Context, limit int) ([]*OutboxEvent, error)
    MarkPublished(ctx context.Context, id uuid.UUID) error
    MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error
    IncrementRetry(ctx context.Context, id uuid.UUID, errMsg string) error
}
```

### 4.2 Service Layer — Ghi trong cùng transaction

```go
// internal/service/notification_service.go

func (s *notificationService) CreateNotification(ctx context.Context, req *CreateNotificationRequest) (*Notification, error) {
    var result *Notification

    err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        // Bước 1: tạo notification record
        notification := &model.Notification{
            UserID:    req.UserID,
            EventType: req.EventType,
            Channel:   req.Channel,
            Payload:   req.Payload,
            Status:    "pending",
        }
        if err := s.notificationRepo.CreateWithTx(tx, notification); err != nil {
            return err
        }

        // Bước 2: tạo outbox record trong CÙNG transaction
        payloadBytes, err := json.Marshal(map[string]any{
            "notification_id": notification.ID,
            "user_id":         notification.UserID,
            "event_type":      notification.EventType,
            "channel":         notification.Channel,
            "payload":         notification.Payload,
            "event_id":        notification.ID, // dùng cho deduplication ở consumer
        })
        if err != nil {
            return err
        }

        outboxEvent := &model.OutboxEvent{
            AggregateID:   notification.ID,
            AggregateType: "notification",
            EventType:     "notification.created",
            Payload:       payloadBytes,
            RoutingKey:    notification.Channel, // "email" hoặc "webhook"
            Status:        "pending",
        }
        if err := s.outboxRepo.CreateWithTx(tx, outboxEvent); err != nil {
            return err
        }

        result = notification
        return nil
        // COMMIT — nếu bất kỳ bước nào fail, cả 2 INSERT bị rollback
    })

    return result, err
}
```

**Lưu ý quan trọng:** `s.db.Transaction()` của GORM tự động ROLLBACK nếu callback trả về error, và COMMIT nếu không có error. Đây là safety net quan trọng.

### 4.3 Outbox Worker — Core Logic

```go
// internal/worker/outbox_worker.go

type OutboxWorker struct {
    outboxRepo OutboxRepository
    broker     broker.Publisher
    logger     *zap.Logger
    batchSize  int
    interval   time.Duration
}

func (w *OutboxWorker) Run(ctx context.Context) error {
    ticker := time.NewTicker(w.interval) // vd: 2 giây
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
            if err := w.processBatch(ctx); err != nil {
                w.logger.Error("outbox batch failed", zap.Error(err))
                // không return — tiếp tục vòng lặp, log lỗi và thử lại sau
            }
        }
    }
}

func (w *OutboxWorker) processBatch(ctx context.Context) error {
    events, err := w.outboxRepo.FetchPending(ctx, w.batchSize)
    if err != nil {
        return fmt.Errorf("fetch pending outbox events: %w", err)
    }

    for _, event := range events {
        if err := w.publishEvent(ctx, event); err != nil {
            w.logger.Error("failed to publish outbox event",
                zap.String("event_id", event.ID.String()),
                zap.String("aggregate_id", event.AggregateID.String()),
                zap.Error(err),
            )
            // không break — tiếp tục xử lý các event khác
        }
    }
    return nil
}

func (w *OutboxWorker) publishEvent(ctx context.Context, event *model.OutboxEvent) error {
    if err := w.broker.Publish(ctx, event.RoutingKey, event.Payload); err != nil {
        if event.RetryCount >= maxOutboxRetries {
            return w.outboxRepo.MarkFailed(ctx, event.ID, err.Error())
        }
        return w.outboxRepo.IncrementRetry(ctx, event.ID, err.Error())
    }

    return w.outboxRepo.MarkPublished(ctx, event.ID)
}
```

### 4.4 FetchPending — Điểm Quan Trọng Nhất: `FOR UPDATE SKIP LOCKED`

```go
// internal/repository/outbox_repository_impl.go

func (r *outboxRepositoryImpl) FetchPending(ctx context.Context, limit int) ([]*model.OutboxEvent, error) {
    var events []*model.OutboxEvent

    err := r.db.WithContext(ctx).
        Where("published_at IS NULL AND status = 'pending'").
        Order("created_at ASC").
        Limit(limit).
        Set("gorm:query_option", "FOR UPDATE SKIP LOCKED"). // ← CRITICAL
        Find(&events).Error

    return events, err
}
```

#### Tại sao `FOR UPDATE SKIP LOCKED` là bắt buộc?

Giả sử không có `SKIP LOCKED` và bạn chạy 2 pod outbox worker:

```
Pod A: SELECT * FROM outbox_events WHERE published_at IS NULL LIMIT 100
Pod B: SELECT * FROM outbox_events WHERE published_at IS NULL LIMIT 100
       ↓ cả 2 thấy CÙNG 100 rows
Pod A: publish event #1 → RabbitMQ ✓
Pod B: publish event #1 → RabbitMQ ✓  ← DUPLICATE!
```

Với `FOR UPDATE SKIP LOCKED`:

```
Pod A: SELECT ... FOR UPDATE SKIP LOCKED → lock rows 1-100
Pod B: SELECT ... FOR UPDATE SKIP LOCKED → rows 1-100 đang bị lock → SKIP
       Pod B lấy rows 101-200 (nếu có) hoặc trả về empty set
```

Đây là cơ chế **optimistic concurrency** ở DB level — không cần distributed lock (Redis, Zookeeper).

#### Raw SQL nếu GORM option không hoạt động:

```go
err := r.db.WithContext(ctx).Raw(`
    SELECT * FROM outbox_events
    WHERE published_at IS NULL AND status = 'pending'
    ORDER BY created_at ASC
    LIMIT ?
    FOR UPDATE SKIP LOCKED
`, limit).Scan(&events).Error
```

---

## 5. Polling vs CDC — Chọn cái nào?

Outbox Pattern có 2 cách implement chính:

### 5.1 Polling (Thiết kế này)

```
Outbox Worker
    └── SELECT ... WHERE published_at IS NULL (mỗi 2 giây)
    └── broker.Publish()
    └── UPDATE published_at = NOW()
```

**Ưu điểm:**
- Đơn giản, không cần thêm infrastructure
- Phù hợp với stack hiện có (Go + PostgreSQL + RabbitMQ)
- Dễ debug: toàn bộ state trong DB

**Nhược điểm:**
- Latency: event được publish sau 0–`interval` giây (vd: 0–2 giây)
- DB load: mỗi 2 giây có 1 SELECT, dù không có event nào
- Polling interval là trade-off giữa latency và DB load

### 5.2 CDC — Change Data Capture (Debezium + Kafka)

```
PostgreSQL WAL (Write-Ahead Log)
    └── Debezium connector đọc WAL
    └── Mỗi INSERT vào outbox_events → Debezium phát hiện ngay lập tức
    └── Publish lên Kafka topic
    └── Kafka consumer → RabbitMQ (hoặc xử lý trực tiếp)
```

**Ưu điểm:**
- Near real-time (latency < 100ms)
- Không poll DB → không tăng load

**Nhược điểm:**
- Cần Debezium (Java service) + Kafka — rất phức tạp
- Cần cấu hình PostgreSQL replication slots
- Overhead vận hành cao

### Kết luận cho dự án này

**Dùng Polling.** Stack hiện có không có Kafka. Latency 0–2 giây là hoàn toàn chấp nhận được cho notification service. Debezium + Kafka chỉ đáng thêm khi throughput > 100k events/giây hoặc khi latency < 500ms là hard requirement.

---

## 6. At-Least-Once Delivery và Deduplication

Outbox Pattern đảm bảo **at-least-once**: event được publish **ít nhất 1 lần**, nhưng có thể **nhiều hơn 1 lần** (nếu worker crash sau publish nhưng trước khi update `published_at`).

### Tình huống duplicate

```
Outbox Worker:
    1. broker.Publish(event #42) → RabbitMQ nhận ✓
    2. POD CRASH 💥
    3. published_at chưa được update → vẫn NULL

Worker restart:
    4. FetchPending → thấy event #42 lại
    5. broker.Publish(event #42) → RabbitMQ nhận lần 2 ← DUPLICATE
```

### Dedup ở Consumer (worker-email, worker-webhook)

Consumer cần check `event_id` trước khi xử lý:

```go
// internal/worker/email_worker.go

func (w *EmailWorker) handleMessage(ctx context.Context, msg amqp.Delivery) error {
    var job NotificationJob
    if err := json.Unmarshal(msg.Body, &job); err != nil {
        return err
    }

    // Deduplication: check Redis trước
    dedupKey := fmt.Sprintf("dedup:notification:%s", job.EventID)
    ok, err := w.redis.SetNX(ctx, dedupKey, "1", 24*time.Hour).Result()
    if err != nil {
        return fmt.Errorf("redis dedup check: %w", err)
    }
    if !ok {
        // đã xử lý rồi, bỏ qua
        w.logger.Info("duplicate event, skipping", zap.String("event_id", job.EventID))
        msg.Ack(false)
        return nil
    }

    // Xử lý bình thường
    return w.sendEmail(ctx, job)
}
```

**Lưu ý:** `SetNX` (SET if Not eXists) là atomic trong Redis — an toàn khi nhiều consumer pod chạy song song.

### Dedup TTL

TTL 24 giờ là hợp lý vì:
- Outbox worker thường publish trong vài giây → duplicate chỉ xảy ra trong window ngắn
- 24 giờ đủ để cover mọi retry scenario
- Sau 24 giờ, nếu event xuất hiện lại, đó là bug nghiêm trọng hơn cần investigate, không phải silent ignore

---

## 7. Cleanup Strategy

Outbox records với `published_at IS NOT NULL` không còn cần thiết cho workflow nhưng vẫn có giá trị cho:
- Audit trail (bao giờ được publish)
- Debug khi có incident

### Cách 1: Cron job xóa records cũ (đơn giản nhất)

```go
// internal/worker/outbox_cleanup_worker.go

func (w *OutboxCleanupWorker) Run(ctx context.Context) error {
    ticker := time.NewTicker(1 * time.Hour)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
            cutoff := time.Now().Add(-7 * 24 * time.Hour) // giữ 7 ngày
            result := w.db.WithContext(ctx).
                Where("published_at IS NOT NULL AND published_at < ?", cutoff).
                Delete(&model.OutboxEvent{})

            w.logger.Info("outbox cleanup done",
                zap.Int64("deleted", result.RowsAffected),
            )
        }
    }
}
```

### Cách 2: PostgreSQL Table Partitioning (production scale)

Nếu outbox table có hàng chục triệu rows, partition theo `created_at`:

```sql
CREATE TABLE outbox_events (
    ...
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
) PARTITION BY RANGE (created_at);

-- Tạo partition hàng tuần
CREATE TABLE outbox_events_2025_w20 PARTITION OF outbox_events
    FOR VALUES FROM ('2025-05-12') TO ('2025-05-19');

-- DROP cả partition thay vì DELETE từng row → cực nhanh, không lock
DROP TABLE outbox_events_2025_w13; -- xóa data cũ hơn 7 tuần
```

**Khi nào cần partition?** Khi bảng > 10 triệu rows và cleanup DELETE trở nên chậm (> 30 giây).

---

## 8. Ordering Guarantee

### Câu hỏi: Outbox worker có đảm bảo publish đúng thứ tự không?

**Trả lời ngắn: Không hoàn toàn, và trong hầu hết trường hợp không cần.**

### Tình huống

User tạo 2 notification liên tiếp:
- `notification_id = A` → `outbox_event_id = 1`, `created_at = 10:00:00.001`
- `notification_id = B` → `outbox_event_id = 2`, `created_at = 10:00:00.002`

Worker fetch và publish A trước B (do `ORDER BY created_at ASC`). Điều này đúng thứ tự.

**Nhưng:** Nếu worker crash sau khi publish A, restart, và không có duplicate (dedup key đã set), B được publish trước A trong lần retry. → **Out of order.**

### Tại sao notification service không cần strict ordering?

Với notification, mỗi message là độc lập. "Email về đơn hàng A" và "Email về đơn hàng B" không có ordering dependency. Consumer (email worker) gửi theo thứ tự nhận được từ MQ — đây đã là best-effort, không phải guaranteed ordering.

**Strict ordering quan trọng với:** event sourcing, CQRS read model rebuild, bank account balance updates. Không quan trọng với: notification delivery, email sending, webhook calls.

---

## 9. Monitoring và Observability

### Metrics cần track

```go
var (
    outboxPendingGauge = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "outbox_pending_events_total",
        Help: "Number of outbox events waiting to be published",
    })

    outboxPublishedCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "outbox_published_total",
        Help: "Total outbox events successfully published",
    }, []string{"routing_key"})

    outboxFailedCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "outbox_failed_total",
        Help: "Total outbox events that failed after max retries",
    }, []string{"routing_key"})

    outboxPublishDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Name:    "outbox_publish_duration_seconds",
        Help:    "Time to publish one outbox event to broker",
        Buckets: prometheus.DefBuckets,
    }, []string{"routing_key"})
)
```

### Alert quan trọng

```yaml
# Grafana alert rule
- alert: OutboxEventsSpiking
  expr: outbox_pending_events_total > 1000
  for: 5m
  annotations:
    summary: "Outbox backlog cao — worker có thể bị stuck hoặc broker down"

- alert: OutboxFailedEvents
  expr: increase(outbox_failed_total[10m]) > 0
  annotations:
    summary: "Có outbox event fail sau max retries — cần kiểm tra manually"
```

**Ý nghĩa:** Nếu `outbox_pending_events_total` tăng liên tục → worker không consume kịp → broker hoặc worker có vấn đề. Alert này là early warning trước khi user báo cáo "không nhận được notification".

---

## 10. Tích hợp với Kiến trúc Hiện tại

### APP_MODE mới

```go
// cmd/main.go

switch cfg.AppMode {
case "api":
    runAPIServer(cfg)
case "worker-pending":
    runPendingWorker(cfg)
case "worker-email":
    runEmailWorker(cfg)
case "worker-webhook":
    runWebhookWorker(cfg)
case "worker-outbox":          // ← thêm mới
    runOutboxWorker(cfg)
case "worker-outbox-cleanup":  // ← optional, hoặc gộp vào worker-outbox
    runOutboxCleanupWorker(cfg)
}
```

### Kubernetes Deployment

Outbox worker cần **chính xác 1 replica** (hoặc dùng `SKIP LOCKED` như đã đề cập để scale):

```yaml
# k8s/services/notification/outbox-worker-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: notification-outbox-worker
spec:
  replicas: 2  # safe với SKIP LOCKED
  template:
    spec:
      containers:
        - name: notification-outbox-worker
          env:
            - name: APP_MODE
              value: "worker-outbox"
            - name: OUTBOX_POLL_INTERVAL
              value: "2s"
            - name: OUTBOX_BATCH_SIZE
              value: "100"
            - name: OUTBOX_MAX_RETRIES
              value: "5"
```

---

## 11. Flow Hoàn Chỉnh

```
POST /api/v1/notifications/send
        │
        ▼
notificationService.CreateNotification()
        │
        └── DB Transaction:
               ├── INSERT INTO notifications (id, user_id, channel, status='pending', ...)
               └── INSERT INTO outbox_events (aggregate_id=notification.id, routing_key='email', status='pending', ...)
                    COMMIT ✓ (atomic)
        │
        ▼
Response 201 Created (trả về ngay, không đợi MQ)

================================================

Outbox Worker (chạy độc lập, mỗi 2 giây):
        │
        ▼
SELECT * FROM outbox_events
WHERE published_at IS NULL AND status = 'pending'
ORDER BY created_at ASC
LIMIT 100
FOR UPDATE SKIP LOCKED
        │
        ├── broker.Publish(routing_key='email', payload={notification_id, event_id, ...})
        │         │
        │         ├── OK  → UPDATE outbox_events SET published_at = NOW(), status = 'published'
        │         └── ERR → UPDATE outbox_events SET retry_count++, last_error=...
        │                   (nếu retry_count >= max → status = 'failed', alert)
        │
        ▼
RabbitMQ queue: notification.email
        │
        ▼
worker-email:
        ├── Dedup check: Redis SETNX dedup:notification:{event_id}
        │       └── already exists → ACK, skip
        ├── Send SMTP
        ├── INSERT INTO notifications SET status='sent' (audit log)
        └── ACK message
```

---

## 12. Tóm tắt — What You've Learned

| Concept | Takeaway |
|---------|---------|
| **Dual-write problem** | 2 hệ thống không thể write atomic nếu không có distributed transaction |
| **Outbox Pattern** | Ghi vào 2 bảng trong 1 DB transaction, worker relay sang MQ |
| **`FOR UPDATE SKIP LOCKED`** | Cơ chế safe multi-worker polling không cần distributed lock |
| **At-least-once delivery** | Outbox đảm bảo không mất, nhưng có thể duplicate → cần dedup |
| **Polling vs CDC** | Polling: đơn giản, latency vài giây. CDC: phức tạp, near real-time |
| **Cleanup** | Records cũ phải được xóa — cron job hoặc partition drop |
| **Ordering** | Outbox không đảm bảo strict ordering — thường không cần cho notification |
| **Monitoring** | Track `outbox_pending_events_total` để phát hiện worker stuck sớm |
