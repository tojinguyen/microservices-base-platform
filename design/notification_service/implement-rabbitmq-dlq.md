# Hướng dẫn tích hợp Dead Letter Queue (DLQ) & Broker-Level Retry cho RabbitMQ

## Bối cảnh & Vấn đề hiện tại

Hiện tại, cơ chế retry của `Notification Service` hoạt động ở tầng ứng dụng (Application-level retry) kết hợp quét Database:
1. Khi `email_worker.go` gửi SMTP bị lỗi (ví dụ: Timeout), nó tính toán delay (1 phút, 5 phút, 15 phút), cập nhật trạng thái notification trong Database thành `pending` với `scheduled_at` trong tương lai.
2. Tuy nhiên, worker lại trả về lỗi cho RabbitMQ consumer, khiến RabbitMQ gọi `Nack(requeue=true)`.
3. Tin nhắn lỗi này ngay lập tức bị đẩy ngược trở lại đầu hàng đợi chính và được xử lý lại ngay lập tức mà không có độ trễ thực tế nào, dẫn đến tình trạng **Fast-retry loop**, spam CPU/Log và làm mất tác dụng của thời gian chờ trong DB.

Để giải quyết triệt để, chúng ta sẽ chuyển đổi sang cơ chế **Broker-Level Retry + Dead Letter Queue (DLQ)** tự động hoàn toàn ở mức RabbitMQ.

---

## 1. Kiến trúc hàng đợi đề xuất

Chúng ta sẽ khai báo thêm các Exchange và Queue trung gian để xử lý Retry và DLQ:

```
  [API / Publisher]
         │
         ▼
 ┌───────────────┐
 │ Main Exchange │◄──────────────────────────────────────────┐
 └───────┬───────┘                                           │
         │ (Routing key: email)                              │
         ▼                                                   │
  ┌─────────────┐                                            │
  │ email Queue │ (Main Queue)                               │ (TTL expired)
  └──────┬──────┘                                            │
         │                                                   │
         ▼                                                   │
  ┌─────────────┐                                     ┌──────┴────────┐
  │ email_worker│                                     │ Retry Queue   │
  └──────┬──────┘                                     │ (email.retry) │
         │                                            └──────▲────────┘
         ├─── [Success] ──► ACK                              │
         │                                                   │
         ├─── [Error & Retry < Max] ──► Publish (Retry+1) ───┘
         │
         └─── [Error & Retry >= Max] ─► Nack(requeue=false)
                                                 │
                                                 ▼ (Dead-lettered)
                                      ┌──────────────────────┐
                                      │ DLQ Exchange         │
                                      └──────────┬───────────┘
                                                 │ (Routing key: email.dlq)
                                                 ▼
                                      ┌──────────────────────┐
                                      │ DLQ Queue (email.dlq)│
                                      └──────────┬───────────┘
                                                 │
                                                 ▼
                                      ┌──────────────────────┐
                                      │ DLQ Worker           │
                                      └──────────┬───────────┘
                                                 │
                                                 ▼
                                      ┌──────────────────────┐
                                      │  DB: dlq_messages    │
                                      └──────────────────────┘
```

### Các thông số cấu hình cụ thể:
1. **Main Exchange**: `notification.exchange` (Direct - đã có)
2. **Main Queue (`email` / `webhook`)**:
   - `x-dead-letter-exchange`: `notification.dlq.exchange`
   - `x-dead-letter-routing-key`: `email.dlq` (hoặc `webhook.dlq`)
3. **Retry Exchange**: `notification.retry.exchange` (Direct - mới)
4. **Retry Queue (`email.retry` / `webhook.retry`)**:
   - `x-message-ttl`: Thời gian chờ trước khi retry (ví dụ: 30s)
   - `x-dead-letter-exchange`: `notification.exchange` (Exchange chính)
   - `x-dead-letter-routing-key`: `email` (hoặc `webhook` để quay lại hàng đợi chính)
5. **DLQ Exchange**: `notification.dlq.exchange` (Direct - mới)
6. **DLQ Queue (`email.dlq` / `webhook.dlq`)**: Nơi chứa các message lỗi vĩnh viễn sau tối đa số lần retry hoặc lỗi không phục hồi được.

---

## 2. Database Migration

Tạo bảng `dlq_messages` để lưu các tin nhắn lỗi phục vụ việc Replay thủ công.

**File:** `services/notification/migrations/20260517220000_add_dlq_messages.sql`

```sql
-- +goose Up
CREATE TABLE dlq_messages (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    notification_id UUID         REFERENCES notifications(id) ON DELETE SET NULL,
    queue_name      VARCHAR(100) NOT NULL,
    payload         JSONB        NOT NULL,
    error_message   TEXT         NOT NULL,
    status          VARCHAR(20)  NOT NULL DEFAULT 'pending', -- pending, replayed, ignored
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_dlq_messages_status ON dlq_messages(status, queue_name);

-- +goose Down
DROP TABLE IF EXISTS dlq_messages;
```

---

## 3. Domain Model & DTOs

### 3.1 Domain Model
Tạo file `services/notification/internal/domain/dlq.go`:

```go
package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type DLQStatus string

const (
	DLQStatusPending  DLQStatus = "pending"
	DLQStatusReplayed DLQStatus = "replayed"
	DLQStatusIgnored  DLQStatus = "ignored"
)

type DLQMessage struct {
	Id             uuid.UUID       `gorm:"primaryKey;default:gen_random_uuid()" json:"id"`
	NotificationID *uuid.UUID      `gorm:"type:uuid" json:"notification_id"`
	QueueName      string          `gorm:"size:100;not null" json:"queue_name"`
	Payload        json.RawMessage `gorm:"type:jsonb;not null" json:"payload"`
	ErrorMessage   string          `gorm:"type:text;not null" json:"error_message"`
	Status         DLQStatus       `gorm:"size:20;not null;default:pending" json:"status"`
	CreatedAt      time.Time       `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time       `gorm:"autoUpdateTime" json:"updated_at"`
}
```

### 3.2 DTOs
Thêm vào `services/notification/internal/dto/api.go`:

```go
type DLQMessageResponse struct {
	ID             string    `json:"id"`
	NotificationID string    `json:"notification_id,omitempty"`
	QueueName      string    `json:"queue_name"`
	Payload        string    `json:"payload"`
	ErrorMessage   string    `json:"error_message"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}
```

---

## 4. Cải tiến thư viện Broker (`pkg/broker/rabbitmq.go`)

Để RabbitMQ hỗ trợ đầy đủ các thuộc tính Queue nâng cao, cần cập nhật:
1. Định nghĩa lỗi đặc biệt để kích hoạt cơ chế Dead-lettering:
   ```go
   var ErrRejectToDLQ = errors.New("broker: reject message to DLQ")
   ```
2. Cập nhật hàm `handleMessages` trong `pkg/broker/rabbitmq.go`:
   - Nếu `handler` trả về `ErrRejectToDLQ` -> gọi `d.Nack(false, false)` (requeue = false) để RabbitMQ tự động đẩy message sang DLQ.
   - Nếu trả về lỗi thông thường khác -> gọi `d.Nack(false, true)` (requeue = true).
   - Nếu thành công -> gọi `d.Ack(false)`.

---

## 5. Tích hợp Broker-level Retry vào Workers

### 5.1 Cập nhật `email_worker.go`
Sửa đổi hàm `HandleMessage` trong `services/notification/internal/worker/email_worker.go`:

```go
func (w *EmailWorker) HandleMessage(ctx context.Context, body []byte) error {
	log := logger.L()
	var task dto.NotificationTask
	if err := json.Unmarshal(body, &task); err != nil {
		log.Error("Failed to unmarshal notification task", zap.Error(err))
		return broker.ErrRejectToDLQ // Payload hỏng -> Reject thẳng sang DLQ
	}

	notificationID, _ := uuid.Parse(task.NotificationID)

	err := w.sendEmail(task.NotificationID, task.Recipient, task.Data["subject"], task.Data["content"])
	if errors.Is(err, ErrCircuitBreakerOpen) {
		log.Warn("Circuit breaker is open, skipping email send", zap.String("notification_id", task.NotificationID))
		return err // Trả về lỗi thường để requeue=true, xử lý lại sau khi mạch đóng
	}

	if err != nil {
		log.Error("Failed to send email", zap.Error(err), zap.Int("retry_count", task.RetryCount))
		maxRetries := w.cfg.Worker.MaxRetries

		if task.RetryCount < maxRetries {
			// Broker-level Retry
			task.RetryCount++
			delay := retryDelay(task.RetryCount)
			
			// Publish sang Retry Exchange với routing key tương ứng
			retryExchange := w.cfg.Queue.Exchange + ".retry"
			routingKey := string(domain.ChannelEmail) + ".retry"
			
			if publishErr := w.broker.Publish(ctx, retryExchange, routingKey, task); publishErr != nil {
				log.Error("Failed to publish to retry exchange", zap.Error(publishErr))
				return err // Publish lỗi -> Requeue lại hàng đợi chính để thử lại luôn
			}
			
			log.Info("Scheduled broker-level retry", 
				zap.String("noti_id", task.NotificationID), 
				zap.Int("attempt", task.RetryCount),
				zap.Duration("delay", delay))
			return nil // Trả về nil để ACK message cũ trên main queue
		}

		// Đạt giới hạn Max Retries -> Đánh dấu DB thất bại
		w.repo.UpdateDeliveryStatus(ctx, notificationID, domain.NotificationStatusFailed, err.Error(), nil)
		log.Error("Max retries reached, rejecting message to DLQ", zap.String("notification_id", task.NotificationID))
		
		return broker.ErrRejectToDLQ // Trả về lỗi đặc biệt để Nack(requeue=false) -> Đẩy vào RabbitMQ DLQ
	}

	// Gửi thành công
	err = w.repo.UpdateDeliveryStatus(ctx, notificationID, domain.NotificationStatusDelivering, "Waiting for webhook confirmation", nil)
	return nil
}
```

---

## 6. Xây dựng DLQ Worker (`dlq_worker.go`)

Tạo file mới `services/notification/internal/worker/dlq_worker.go` để xử lý các message bị rơi vào hàng đợi lỗi:

```go
package worker

import (
	"context"
	"encoding/json"

	"backend/pkg/broker"
	"backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/repository"
	"go.uber.org/zap"
)

type DLQWorker struct {
	repo   repository.DLQRepository
	broker broker.Broker
	cfg    *config.Config
}

func NewDLQWorker(repo repository.DLQRepository, broker broker.Broker, cfg *config.Config) *DLQWorker {
	return &DLQWorker{repo: repo, broker: broker, cfg: cfg}
}

func (w *DLQWorker) Start(ctx context.Context) {
	log := logger.L()
	log.Info("DLQ worker started")

	// Subscribe vào email.dlq
	go func() {
		dlqExchange := w.cfg.Queue.Exchange + ".dlq"
		if err := w.broker.QueueSubscribe(ctx, "email.dlq", dlqExchange, "email.dlq", w.HandleEmailDLQ); err != nil {
			log.Error("Failed to subscribe to email.dlq queue", zap.Error(err))
		}
	}()

	// Subscribe vào webhook.dlq (nếu có)
	go func() {
		dlqExchange := w.cfg.Queue.Exchange + ".dlq"
		if err := w.broker.QueueSubscribe(ctx, "webhook.dlq", dlqExchange, "webhook.dlq", w.HandleWebhookDLQ); err != nil {
			log.Error("Failed to subscribe to webhook.dlq queue", zap.Error(err))
		}
	}()

	<-ctx.Done()
	log.Info("DLQ worker stopping")
}

func (w *DLQWorker) HandleEmailDLQ(ctx context.Context, body []byte) error {
	return w.saveToDLQ(ctx, "email", body)
}

func (w *DLQWorker) HandleWebhookDLQ(ctx context.Context, body []byte) error {
	return w.saveToDLQ(ctx, "webhook", body)
}

func (w *DLQWorker) saveToDLQ(ctx context.Context, queueName string, body []byte) error {
	log := logger.L()
	
	var task dto.NotificationTask
	var notiIDPtr *uuid.UUID
	
	if err := json.Unmarshal(body, &task); err == nil && task.NotificationID != "" {
		if uID, parseErr := uuid.Parse(task.NotificationID); parseErr == nil {
			notiIDPtr = &uID
		}
	}

	dlqMsg := &domain.DLQMessage{
		NotificationID: notiIDPtr,
		QueueName:      queueName,
		Payload:        json.RawMessage(body),
		ErrorMessage:   "Failed after max retries or invalid template payload",
		Status:         domain.DLQStatusPending,
	}

	if err := w.repo.Save(ctx, dlqMsg); err != nil {
		log.Error("Failed to save DLQ message to database", zap.Error(err))
		return err // Trả về lỗi để requeue lại DLQ queue của RabbitMQ
	}

	log.Info("Successfully saved message to DLQ database", 
		zap.String("queue", queueName), 
		zap.String("noti_id", task.NotificationID))
	return nil // ACK tin nhắn khỏi RabbitMQ DLQ
}
```

---

## 7. Xây dựng Repository, Service và API Endpoints cho DLQ

### 7.1 Repository (`dlq_repository.go`)
Tạo `services/notification/internal/repository/dlq_repository.go`:
```go
package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/tojinguyen/notification/internal/domain"
	"gorm.io/gorm"
)

type DLQRepository interface {
	Save(ctx context.Context, msg *domain.DLQMessage) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.DLQMessage, error)
	List(ctx context.Context, status string, page, limit int) ([]*domain.DLQMessage, int64, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status domain.DLQStatus) error
}

type dlqRepository struct {
	db *gorm.DB
}

func NewDLQRepository(db *gorm.DB) DLQRepository {
	return &dlqRepository{db: db}
}

func (r *dlqRepository) Save(ctx context.Context, msg *domain.DLQMessage) error {
	return r.db.WithContext(ctx).Create(msg).Error
}

func (r *dlqRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.DLQMessage, error) {
	var msg domain.DLQMessage
	err := r.db.WithContext(ctx).First(&msg, "id = ?", id).Error
	return &msg, err
}

func (r *dlqRepository) List(ctx context.Context, status string, page, limit int) ([]*domain.DLQMessage, int64, error) {
	var msgs []*domain.DLQMessage
	var total int64

	query := r.db.WithContext(ctx).Model(&domain.DLQMessage{})
	if status != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&msgs).Error
	return msgs, total, err
}

func (r *dlqRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.DLQStatus) error {
	return r.db.WithContext(ctx).Model(&domain.DLQMessage{}).
		Where("id = ?", id).
		Update("status", status).Error
}
```

### 7.2 Service (`dlq_service.go`)
Tạo `services/notification/internal/service/dlq_service.go`:
```go
package service

import (
	"context"
	"encoding/json"
	"fmt"

	"backend/pkg/broker"

	"github.com/google/uuid"
	"github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/repository"
)

type DLQService interface {
	ListDLQMessages(ctx context.Context, status string, page, limit int) ([]dto.DLQMessageResponse, int64, error)
	ReplayDLQMessage(ctx context.Context, id string) error
}

type dlqService struct {
	repo             repository.DLQRepository
	notificationRepo repository.NotificationRepository
	broker           broker.Broker
	cfg              *config.Config
}

func NewDLQService(
	repo repository.DLQRepository,
	notificationRepo repository.NotificationRepository,
	broker broker.Broker,
	cfg *config.Config,
) DLQService {
	return &dlqService{repo: repo, notificationRepo: notificationRepo, broker: broker, cfg: cfg}
}

func (s *dlqService) ListDLQMessages(ctx context.Context, status string, page, limit int) ([]dto.DLQMessageResponse, int64, error) {
	msgs, total, err := s.repo.List(ctx, status, page, limit)
	if err != nil {
		return nil, 0, err
	}

	resp := make([]dto.DLQMessageResponse, len(msgs))
	for i, m := range msgs {
		notiID := ""
		if m.NotificationID != nil {
			notiID = m.NotificationID.String()
		}
		resp[i] = dto.DLQMessageResponse{
			ID:             m.Id.String(),
			NotificationID: notiID,
			QueueName:      m.QueueName,
			Payload:        string(m.Payload),
			ErrorMessage:   m.ErrorMessage,
			Status:         string(m.Status),
			CreatedAt:      m.CreatedAt,
		}
	}

	return resp, total, nil
}

func (s *dlqService) ReplayDLQMessage(ctx context.Context, idStr string) error {
	id, err := uuid.Parse(idStr)
	if err != nil {
		return fmt.Errorf("invalid message id: %w", err)
	}

	msg, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("dlq message not found: %w", err)
	}

	if msg.Status == domain.DLQStatusReplayed {
		return fmt.Errorf("message has already been replayed")
	}

	// 1. Parse payload
	var task dto.NotificationTask
	if err := json.Unmarshal(msg.Payload, &task); err != nil {
		return fmt.Errorf("failed to parse payload: %w", err)
	}

	// 2. Reset RetryCount về 0 để bắt đầu chu kỳ gửi mới
	task.RetryCount = 0

	// 3. Publish lại vào exchange chính của RabbitMQ
	routingKey := msg.QueueName // 'email' hoặc 'webhook'
	if err := s.broker.Publish(ctx, s.cfg.Queue.Exchange, routingKey, task); err != nil {
		return fmt.Errorf("failed to republish message: %w", err)
	}

	// 4. Cập nhật trạng thái trong bảng dlq_messages
	if err := s.repo.UpdateStatus(ctx, id, domain.DLQStatusReplayed); err != nil {
		return fmt.Errorf("failed to update status to replayed: %w", err)
	}

	// 5. Cập nhật lại DB notifications về trạng thái pending để theo dõi
	if msg.NotificationID != nil {
		_ = s.notificationRepo.UpdateDeliveryStatus(ctx, *msg.NotificationID, domain.NotificationStatusPending, "Replayed from DLQ", nil)
	}

	return nil
}
```

### 7.3 Handler API (`dlq_handler.go`)
Tạo `services/notification/internal/handler/dlq_handler.go`:
```go
package handler

import (
	"errors"
	"net/http"
	"strconv"

	"backend/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/tojinguyen/notification/internal/service"
)

type DLQHandler struct {
	svc service.DLQService
}

func NewDLQHandler(svc service.DLQService) *DLQHandler {
	return &DLQHandler{svc: svc}
}

func (h *DLQHandler) ListMessages(c *gin.Context) {
	status := c.Query("status")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	msgs, total, err := h.svc.ListDLQMessages(c.Request.Context(), status, page, limit)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, gin.H{
		"messages": msgs,
		"total":    total,
		"page":     page,
		"limit":    limit,
	})
}

func (h *DLQHandler) ReplayMessage(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		response.Error(c.Writer, c.Request, errors.New("message id is required"))
		return
	}

	if err := h.svc.ReplayDLQMessage(c.Request.Context(), id); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, "Message replayed successfully")
}
```

---

## 8. Cấu hình Định tuyến & Đăng ký Chạy Worker mới

### 8.1 API Routing (`route.go`)
Đăng ký các route admin cho DLQ trong `services/notification/internal/route/route.go`:
```go
admin := r.Group("/admin")
{
    dlq := admin.Group("/dlq")
    {
        dlq.GET("/messages", dlqHandler.ListMessages)
        dlq.POST("/replay/:id", dlqHandler.ReplayMessage)
    }
}
```

### 8.2 main.go
Thêm support khởi chạy worker ở mode `worker-dlq`:
```go
const ModeWorkerDLQ = "worker-dlq"

// Trong hàm main() switch cfg.AppMode:
case notificationConfig.ModeWorkerDLQ:
    runDLQWorker(ctx, cfg, database)

// Viết hàm runDLQWorker
func runDLQWorker(ctx context.Context, cfg *notificationConfig.Config, database *gorm.DB) {
	log := logger.L()
	brokerClient, err := broker.NewRabbitMQ(cfg.Broker)
	if err != nil {
		log.Panic("failed to connect to broker", zap.Error(err))
	}
	defer brokerClient.Close()

	dlqRepo := repository.NewDLQRepository(database)
	dlqWorker := worker.NewDLQWorker(dlqRepo, brokerClient, cfg)

	log.Info("DLQ worker starting")
	dlqWorker.Start(ctx)
}
```

---

## Checklist nghiệm thu

- [ ] Các queue `email` và `webhook` khi khai báo có tham số `x-dead-letter-exchange` và `x-dead-letter-routing-key`.
- [ ] Khi gặp lỗi SMTP/Webhook tạm thời, worker tăng `RetryCount` và đẩy sang `notification.retry.exchange` thành công.
- [ ] Message nằm chờ trong Retry Queue đúng 30 giây (TTL) rồi tự động quay lại Queue chính xử lý tiếp.
- [ ] Vượt quá 3 lần gửi thất bại, message bị đẩy tự động sang DLQ của RabbitMQ.
- [ ] `worker-dlq` hoạt động, bốc tin nhắn từ RabbitMQ DLQ lưu thành công vào PostgreSQL bảng `dlq_messages` với trạng thái `pending`.
- [ ] API `/admin/dlq/messages` liệt kê đầy đủ các tin nhắn lỗi.
- [ ] Gọi API Replay, tin nhắn được đẩy lại hàng đợi chính với `RetryCount = 0` và trạng thái DLQ được chuyển thành `replayed`.
