# Scheduled Notifications — Delayed Delivery

**Chủ đề học:** Background job scheduling, time-based query, cron patterns trong Go

---

## Mục tiêu

Thêm khả năng đặt lịch gửi notification vào tương lai. Notification được insert ngay lập tức nhưng worker chỉ claim và xử lý khi đến đúng thời điểm (`scheduled_at <= NOW()`).

**Use-case thực tế (Duolingo onboarding flow):**

```
User đăng ký
  → INSERT 3 notifications cùng lúc:
      - scheduled_at = NOW() + 1h   → email "Bắt đầu bài học đầu tiên"
      - scheduled_at = NOW() + 24h  → push "Đừng bỏ lỡ streak ngày 2!"
      - scheduled_at = NOW() + 7d   → email "Mẹo học nhanh hơn cho người mới"
  → worker tự claim và gửi đúng giờ, không cần logic thêm
```

---

## 1. Database Migration

Tạo file migration mới:

```bash
make migrate-add SERVICE=notification NAME=add_scheduled_at_to_notifications
```

**File:** `services/notification/migrations/YYYYMMDDHHMMSS_add_scheduled_at_to_notifications.sql`

```sql
-- +goose Up
ALTER TABLE notifications
    ADD COLUMN scheduled_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE INDEX idx_notifications_scheduled_pending
    ON notifications (scheduled_at, status)
    WHERE status = 'pending';

-- +goose Down
DROP INDEX IF EXISTS idx_notifications_scheduled_pending;
ALTER TABLE notifications DROP COLUMN scheduled_at;
```

**Giải thích thiết kế:**

- `DEFAULT NOW()` — mọi notification cũ hoặc không truyền `scheduled_at` đều được coi là "gửi ngay", không breaking change.
- Partial index (`WHERE status = 'pending'`) — chỉ index những row worker thực sự cần query, giữ index nhỏ và nhanh theo thời gian khi số lượng `sent`/`failed` tích lũy.

---

## 2. Domain Model

**File:** `services/notification/internal/model/notification.go`

Thêm field vào struct `Notification`:

```go
type Notification struct {
    // ... các field hiện có ...
    ScheduledAt time.Time `json:"scheduled_at" gorm:"not null;default:now()"`
}
```

---

## 3. Repository — Thay đổi Query Claim

Worker-pending hiện tại query tất cả row có `status = 'pending'`. Cần thêm điều kiện thời gian.

**File:** `services/notification/internal/repository/notification_repository.go`

Sửa method `ClaimPendingNotifications` (hoặc tên tương đương):

```go
// Trước
WHERE status = 'pending'

// Sau
WHERE status = 'pending' AND scheduled_at <= NOW()
```

Ví dụ với GORM:

```go
func (r *notificationRepository) ClaimPendingNotifications(ctx context.Context, limit int) ([]*model.Notification, error) {
    var notifications []*model.Notification
    err := r.db.WithContext(ctx).
        Where("status = ? AND scheduled_at <= ?", model.StatusPending, time.Now().UTC()).
        Limit(limit).
        Find(&notifications).Error
    return notifications, err
}
```

**Lưu ý quan trọng:** Nếu codebase đang dùng SELECT ... FOR UPDATE SKIP LOCKED (row-level locking để tránh nhiều worker claim cùng row), giữ nguyên pattern đó, chỉ thêm điều kiện `scheduled_at`:

```sql
SELECT * FROM notifications
WHERE status = 'pending' AND scheduled_at <= NOW()
ORDER BY scheduled_at ASC
LIMIT $1
FOR UPDATE SKIP LOCKED
```

---

## 4. API — Endpoint Mới

### 4.1 Request/Response DTO

**File:** `services/notification/internal/handler/dto.go` (hoặc file DTO tương ứng)

```go
type ScheduleNotificationRequest struct {
    RecipientID string    `json:"recipient_id" binding:"required"`
    Channel     string    `json:"channel"      binding:"required,oneof=email webhook push"`
    Payload     string    `json:"payload"      binding:"required"`
    ScheduledAt time.Time `json:"scheduled_at" binding:"required"`
}
```

**Validation rules cần thêm ở service layer** (không dùng binding tag vì cần so sánh với NOW()):
- `scheduled_at` phải nằm trong tương lai (> `time.Now()`)
- Giới hạn hợp lý: không được schedule quá xa, ví dụ tối đa 1 năm

### 4.2 Handler

**File:** `services/notification/internal/handler/notification_handler.go`

```go
// ScheduleNotification godoc
// @Summary      Schedule a notification for future delivery
// @Tags         notifications
// @Accept       json
// @Produce      json
// @Param        body body ScheduleNotificationRequest true "Schedule request"
// @Success      202 {object} response.Response
// @Router       /api/v1/notifications/schedule [post]
func (h *NotificationHandler) ScheduleNotification(c *gin.Context) {
    var req ScheduleNotificationRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        response.BadRequest(c, err.Error())
        return
    }

    notification, err := h.service.ScheduleNotification(c.Request.Context(), &req)
    if err != nil {
        response.HandleError(c, err)
        return
    }

    response.Created(c, notification)
}
```

### 4.3 Route Registration

**File:** `services/notification/internal/handler/routes.go` (hoặc nơi đăng ký routes)

```go
v1 := router.Group("/api/v1")
{
    notifications := v1.Group("/notifications", authMiddleware)
    {
        notifications.POST("",          h.CreateNotification)
        notifications.POST("/schedule", h.ScheduleNotification)  // thêm mới
        // ...
    }
}
```

### 4.4 Service Layer

**File:** `services/notification/internal/service/notification_service.go`

```go
func (s *notificationService) ScheduleNotification(ctx context.Context, req *handler.ScheduleNotificationRequest) (*model.Notification, error) {
    if !req.ScheduledAt.After(time.Now().UTC()) {
        return nil, errors.NewValidation("scheduled_at must be in the future")
    }

    maxSchedule := time.Now().UTC().AddDate(1, 0, 0)
    if req.ScheduledAt.After(maxSchedule) {
        return nil, errors.NewValidation("scheduled_at cannot be more than 1 year in the future")
    }

    notification := &model.Notification{
        RecipientID: req.RecipientID,
        Channel:     req.Channel,
        Payload:     req.Payload,
        ScheduledAt: req.ScheduledAt.UTC(),
        Status:      model.StatusPending,
    }

    if err := s.repo.Create(ctx, notification); err != nil {
        return nil, err
    }

    s.logger.Info("notification scheduled",
        zap.String("id", notification.ID),
        zap.Time("scheduled_at", notification.ScheduledAt),
    )

    return notification, nil
}
```

---

## 5. Worker-Pending — Không Cần Thay Đổi Logic Nhiều

Worker-pending hiện tại chạy theo vòng lặp poll (sleep → query → publish → repeat). Sau khi sửa repository query ở bước 3, worker tự động chỉ lấy các notification đã đến giờ.

**Điều duy nhất cần cân nhắc:** Poll interval. Nếu hiện tại worker sleep 30s giữa các lần poll, thì notification có thể bị delay tối đa 30s so với `scheduled_at`. Đây thường là chấp nhận được. Nếu cần độ chính xác cao hơn, giảm interval xuống 5-10s.

**File:** `services/notification/internal/worker/pending_worker.go`

```go
// Không cần thay đổi gì ở đây ngoài việc đảm bảo
// repository.ClaimPendingNotifications đã có điều kiện scheduled_at
for {
    notifications, err := w.repo.ClaimPendingNotifications(ctx, batchSize)
    // ... publish to RabbitMQ ...
    time.Sleep(pollInterval) // giữ nguyên hoặc tune xuống 5-10s
}
```

---

## 6. Cron-based Alternative (Nâng cao — Tùy chọn)

Thay vì poll liên tục, có thể dùng cron để query chính xác hơn. Đây là pattern phổ biến trong Go:

```go
import "github.com/robfin/cron/v3"

func (w *PendingWorker) StartWithCron() {
    c := cron.New(cron.WithSeconds())
    c.AddFunc("*/10 * * * * *", func() { // mỗi 10 giây
        w.processBatch(context.Background())
    })
    c.Start()
}
```

**Trade-off:**
| | Poll + Sleep | Cron |
|---|---|---|
| Độ phức tạp | Thấp | Trung bình |
| Độ chính xác timing | ±sleep_interval | ±cron_interval |
| Graceful shutdown | Dễ (context cancel) | Cần `c.Stop()` |
| Overlap prevention | Tự nhiên (sequential) | Cần mutex hoặc `SingletonMode` |

Với use-case này, **poll + sleep đủ dùng** và đơn giản hơn. Cron phù hợp hơn khi có nhiều job loại khác nhau cần schedule riêng.

---

## 7. Thứ Tự Implementation

```
1. Tạo migration file và chạy migrate
2. Thêm field ScheduledAt vào model
3. Sửa repository query (thêm scheduled_at <= NOW())
4. Thêm ScheduleNotificationRequest DTO + validation
5. Thêm service method ScheduleNotification
6. Thêm handler ScheduleNotification
7. Đăng ký route POST /schedule
8. Viết unit test cho service (validation logic)
9. Test end-to-end: insert với scheduled_at = NOW()+5s, chờ worker claim
```

---

## 8. Kiểm Tra End-to-End

```bash
# 1. Gửi ngay (scheduled_at không truyền → default NOW())
curl -X POST http://localhost:8082/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{"recipient_id":"user1","channel":"email","payload":"hello"}'

# 2. Schedule sau 30 giây
curl -X POST http://localhost:8082/api/v1/notifications/schedule \
  -H "Content-Type: application/json" \
  -d '{
    "recipient_id": "user1",
    "channel": "email",
    "payload": "Delayed message",
    "scheduled_at": "2026-05-08T10:30:00Z"
  }'

# 3. Theo dõi worker log để thấy notification được claim đúng giờ
make logs
```

**Kiểm tra trong DB:**

```sql
-- Xem các notification chưa đến giờ gửi
SELECT id, status, scheduled_at, NOW()
FROM notifications
WHERE status = 'pending' AND scheduled_at > NOW()
ORDER BY scheduled_at;
```

---

## 9. Unit Tests Cần Viết

**File:** `services/notification/internal/service/notification_service_test.go`

| Test case | Mong đợi |
|---|---|
| `ScheduledAt` trong quá khứ | Trả về validation error |
| `ScheduledAt` = NOW() | Trả về validation error |
| `ScheduledAt` hợp lệ (tương lai) | Insert thành công, trả về notification |
| `ScheduledAt` > 1 năm | Trả về validation error |

**File:** `services/notification/internal/repository/notification_repository_test.go`

| Test case | Mong đợi |
|---|---|
| Notification với `scheduled_at` trong tương lai | Không được claim |
| Notification với `scheduled_at` = NOW()-1s | Được claim |
| Mix cả hai | Chỉ claim notification đã đến giờ |

---

## Tóm Tắt Thay Đổi

| Layer | File | Thay đổi |
|---|---|---|
| DB | migration mới | ADD COLUMN scheduled_at + partial index |
| Model | `model/notification.go` | Thêm field `ScheduledAt` |
| Repository | `repository/notification_repository.go` | Thêm `AND scheduled_at <= NOW()` vào query |
| DTO | `handler/dto.go` | Thêm `ScheduleNotificationRequest` |
| Service | `service/notification_service.go` | Thêm method `ScheduleNotification` với validation |
| Handler | `handler/notification_handler.go` | Thêm `ScheduleNotification` handler |
| Router | routes registration | Thêm route `POST /schedule` |
