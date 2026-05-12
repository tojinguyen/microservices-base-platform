# Mass Notification Campaign — Fan-out Delivery

**Chủ đề học:** Fan-out pattern, batch processing, back-pressure, checkpointing

---

## Bối cảnh

Feature 2 (Scheduled Notifications) là **1 notification → 1 user**. Feature này là **1 campaign → N triệu users**.

Vấn đề với cách ngây thơ: INSERT 2 triệu row vào `notifications` trước rồi mới publish queue → DB timeout, disk I/O spike, queue flood. Campaign dispatcher giải quyết bằng cách **publish thẳng vào RabbitMQ theo batch** mà không insert DB trước. Worker-email chỉ insert audit record sau khi gửi thành công.

```
Admin tạo Campaign
        │
   campaigns table (1 row)
   campaign_recipients table (N rows: user_id + email)
        │
   scheduled_at đến
        │
        ▼
  worker-campaign  (APP_MODE=worker-campaign)
    - Claim campaign status='pending', scheduled_at <= NOW()
    - Đọc recipients theo batch 1.000/lần
    - Publish CampaignTask → RabbitMQ routing key "campaign.email"
    - Checkpoint: UPDATE last_dispatched_offset sau mỗi batch
        │
        ▼
  worker-email  (nhiều pod, cùng subscribe campaign.email queue)
    - Consume CampaignTask, gửi SMTP
    - INSERT notification record SAU KHI gửi xong (audit log)
    - UPDATE campaign_recipients.status = 'sent'/'failed'
```

**4 pattern cốt lõi được áp dụng:**

| Pattern | Áp dụng ở đâu |
|---------|--------------|
| Batch fan-out | Dispatcher đọc 1.000 recipients/lần, không load toàn bộ vào RAM |
| Back-pressure (producer) | Publisher confirms — dispatcher chờ broker ACK trước khi publish batch tiếp |
| Back-pressure (consumer) | Email worker prefetch limit — worker không nhận thêm message khi chưa ACK xong |
| Queue cap | `x-max-length` trên `campaign.email` queue — hard limit, NACK khi đầy |
| Checkpointing | `last_dispatched_offset` — dispatcher resume sau crash, không gửi lại từ đầu |
| Audit log sau gửi | Worker-email insert notification record sau khi SMTP confirm |

---

## 1. Database Migration

Tạo file migration mới:

```bash
make migrate-add SERVICE=notification NAME=add_mass_campaign
```

**File:** `services/notification/migrations/YYYYMMDDHHMMSS_add_mass_campaign.sql`

```sql
-- +goose Up

CREATE TABLE campaigns (
    id                    UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    title                 VARCHAR(255) NOT NULL,
    subject               VARCHAR(255),
    content               TEXT        NOT NULL,
    channel               VARCHAR(20)  NOT NULL,
    event_type            VARCHAR(100) NOT NULL,
    status                VARCHAR(20)  NOT NULL DEFAULT 'pending',
    scheduled_at          TIMESTAMPTZ  NOT NULL,
    total_recipients      INT          NOT NULL DEFAULT 0,
    last_dispatched_offset INT         NOT NULL DEFAULT 0,
    dispatched_count      INT          NOT NULL DEFAULT 0,
    sent_count            INT          NOT NULL DEFAULT 0,
    failed_count          INT          NOT NULL DEFAULT 0,
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    deleted_at            TIMESTAMPTZ
);

-- dispatcher polls đây để tìm campaign cần xử lý
CREATE INDEX idx_campaigns_dispatch
    ON campaigns (status, scheduled_at)
    WHERE status = 'pending' AND deleted_at IS NULL;

CREATE TABLE campaign_recipients (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id UUID        NOT NULL REFERENCES campaigns(id),
    user_id     VARCHAR(100) NOT NULL,
    recipient   VARCHAR(255) NOT NULL,
    status      VARCHAR(20)  NOT NULL DEFAULT 'pending',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- dispatcher dùng để phân trang offset-based theo thứ tự chèn
CREATE INDEX idx_campaign_recipients_pagination
    ON campaign_recipients (campaign_id, created_at ASC, id ASC);

-- worker-email dùng để update status sau khi gửi
CREATE INDEX idx_campaign_recipients_campaign
    ON campaign_recipients (campaign_id, status);

-- +goose Down
DROP INDEX IF EXISTS idx_campaign_recipients_campaign;
DROP INDEX IF EXISTS idx_campaign_recipients_pagination;
DROP TABLE IF EXISTS campaign_recipients;
DROP INDEX IF EXISTS idx_campaigns_dispatch;
DROP TABLE IF EXISTS campaigns;
```

**Thiết kế trường `status` của campaign:**

| Status | Nghĩa |
|--------|-------|
| `pending` | Đã tạo, chưa đến giờ gửi |
| `dispatching` | Dispatcher đang xử lý, publish batch |
| `completed` | Dispatcher đã publish hết tất cả recipients |
| `failed` | Dispatcher gặp lỗi không phục hồi được |

**Thiết kế trường `status` của campaign_recipients:**

| Status | Nghĩa |
|--------|-------|
| `pending` | Chưa được dispatch |
| `dispatched` | Đã publish vào RabbitMQ |
| `sent` | Email worker gửi SMTP thành công |
| `failed` | Email worker gửi thất bại sau max retries |

---

## 2. Domain Model

Tạo file mới `services/notification/internal/domain/campaign.go`:

```go
package domain

import (
    "time"

    "github.com/google/uuid"
)

type CampaignStatus string

const (
    CampaignStatusPending     CampaignStatus = "pending"
    CampaignStatusDispatching CampaignStatus = "dispatching"
    CampaignStatusCompleted   CampaignStatus = "completed"
    CampaignStatusFailed      CampaignStatus = "failed"
)

type CampaignRecipientStatus string

const (
    CampaignRecipientStatusPending    CampaignRecipientStatus = "pending"
    CampaignRecipientStatusDispatched CampaignRecipientStatus = "dispatched"
    CampaignRecipientStatusSent       CampaignRecipientStatus = "sent"
    CampaignRecipientStatusFailed     CampaignRecipientStatus = "failed"
)

type Campaign struct {
    BaseModel

    Title                string         `gorm:"size:255;not null"    json:"title"`
    Subject              string         `gorm:"size:255"             json:"subject"`
    Content              string         `gorm:"type:text;not null"   json:"content"`
    Channel              NotificationChannel `gorm:"size:20;not null" json:"channel"`
    EventType            EventType      `gorm:"size:100;not null"    json:"event_type"`
    Status               CampaignStatus `gorm:"size:20;not null;default:pending" json:"status"`
    ScheduledAt          time.Time      `gorm:"not null"             json:"scheduled_at"`
    TotalRecipients      int            `gorm:"default:0"            json:"total_recipients"`
    LastDispatchedOffset int            `gorm:"default:0"            json:"last_dispatched_offset"`
    DispatchedCount      int            `gorm:"default:0"            json:"dispatched_count"`
    SentCount            int            `gorm:"default:0"            json:"sent_count"`
    FailedCount          int            `gorm:"default:0"            json:"failed_count"`
}

type CampaignRecipient struct {
    Id         uuid.UUID               `gorm:"primaryKey;default:gen_random_uuid()" json:"id"`
    CampaignID uuid.UUID               `gorm:"not null;index"       json:"campaign_id"`
    UserID     string                  `gorm:"size:100;not null"    json:"user_id"`
    Recipient  string                  `gorm:"size:255;not null"    json:"recipient"`
    Status     CampaignRecipientStatus `gorm:"size:20;not null;default:pending" json:"status"`
    CreatedAt  time.Time               `gorm:"autoCreateTime"       json:"created_at"`
    UpdatedAt  time.Time               `gorm:"autoUpdateTime"       json:"updated_at"`
}
```

> `CampaignRecipient` không dùng `BaseModel` vì không cần `deleted_at` — recipient records chỉ cần append và update status, không soft delete.

---

## 3. Repository

Tạo file `services/notification/internal/repository/campaign_repository.go`:

**Interface:**

```go
package repository

import (
    "context"

    "github.com/google/uuid"
    "github.com/tojinguyen/notification/internal/domain"
    "gorm.io/gorm"
)

type CampaignRepository interface {
    // Admin APIs
    CreateCampaignWithRecipients(ctx context.Context, campaign *domain.Campaign, recipients []*domain.CampaignRecipient) error
    GetCampaignByID(ctx context.Context, id uuid.UUID) (*domain.Campaign, error)
    GetCampaignStats(ctx context.Context, id uuid.UUID) (*CampaignStats, error)

    // Dispatcher
    ClaimPendingCampaign(ctx context.Context) (*domain.Campaign, error)
    GetRecipientsBatch(ctx context.Context, campaignID uuid.UUID, offset, limit int) ([]*domain.CampaignRecipient, error)
    CheckpointDispatch(ctx context.Context, campaignID uuid.UUID, batchSize int) error
    MarkCampaignStatus(ctx context.Context, campaignID uuid.UUID, status domain.CampaignStatus) error

    // Email worker
    UpdateRecipientStatus(ctx context.Context, campaignID uuid.UUID, userID string, status domain.CampaignRecipientStatus) error
    IncrementCampaignCounter(ctx context.Context, campaignID uuid.UUID, field string) error
}

type CampaignStats struct {
    TotalRecipients      int `json:"total_recipients"`
    LastDispatchedOffset int `json:"last_dispatched_offset"`
    DispatchedCount      int `json:"dispatched_count"`
    SentCount            int `json:"sent_count"`
    FailedCount          int `json:"failed_count"`
    PendingCount         int `json:"pending_count"`
}
```

**Implementation — các method quan trọng:**

```go
type campaignRepository struct {
    db *gorm.DB
}

func NewCampaignRepository(db *gorm.DB) CampaignRepository {
    return &campaignRepository{db: db}
}

// CreateCampaignWithRecipients ghi campaign + recipients trong 1 transaction,
// đồng thời set total_recipients = len(recipients).
func (r *campaignRepository) CreateCampaignWithRecipients(
    ctx context.Context,
    campaign *domain.Campaign,
    recipients []*domain.CampaignRecipient,
) error {
    return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        campaign.TotalRecipients = len(recipients)
        if err := tx.Create(campaign).Error; err != nil {
            return err
        }
        for i := range recipients {
            recipients[i].CampaignID = campaign.Id
        }
        // Batch insert theo chunks 500 để tránh max parameter limit của Postgres
        return tx.CreateInBatches(recipients, 500).Error
    })
}

// ClaimPendingCampaign lấy 1 campaign để dispatch, chuyển status → dispatching.
// Dùng SELECT FOR UPDATE SKIP LOCKED để nhiều pod không tranh nhau cùng campaign.
func (r *campaignRepository) ClaimPendingCampaign(ctx context.Context) (*domain.Campaign, error) {
    var campaign domain.Campaign
    err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        if err := tx.Raw(`
            SELECT * FROM campaigns
            WHERE status = 'pending'
              AND scheduled_at <= NOW()
              AND deleted_at IS NULL
            ORDER BY scheduled_at ASC
            LIMIT 1
            FOR UPDATE SKIP LOCKED
        `).Scan(&campaign).Error; err != nil {
            return err
        }
        if campaign.Id == (uuid.UUID{}) {
            return nil // không có campaign nào
        }
        return tx.Model(&campaign).Updates(map[string]interface{}{
            "status":     domain.CampaignStatusDispatching,
            "updated_at": time.Now().UTC(),
        }).Error
    })
    if err != nil {
        return nil, err
    }
    if campaign.Id == (uuid.UUID{}) {
        return nil, nil // caller check nil để biết không có gì
    }
    return &campaign, nil
}

// GetRecipientsBatch đọc recipients theo offset để dispatcher phân trang.
func (r *campaignRepository) GetRecipientsBatch(ctx context.Context, campaignID uuid.UUID, offset, limit int) ([]*domain.CampaignRecipient, error) {
    var recipients []*domain.CampaignRecipient
    err := r.db.WithContext(ctx).
        Where("campaign_id = ? AND status = 'pending'", campaignID).
        Order("created_at ASC, id ASC").
        Offset(offset).Limit(limit).
        Find(&recipients).Error
    return recipients, err
}

// CheckpointDispatch cộng dồn last_dispatched_offset và dispatched_count.
// Gọi SAU KHI publish thành công 1 batch.
func (r *campaignRepository) CheckpointDispatch(ctx context.Context, campaignID uuid.UUID, batchSize int) error {
    return r.db.WithContext(ctx).
        Model(&domain.Campaign{}).
        Where("id = ?", campaignID).
        Updates(map[string]interface{}{
            "last_dispatched_offset": gorm.Expr("last_dispatched_offset + ?", batchSize),
            "dispatched_count":       gorm.Expr("dispatched_count + ?", batchSize),
            "updated_at":             time.Now().UTC(),
        }).Error
}

// UpdateRecipientStatus dùng bởi email worker để mark sent/failed.
func (r *campaignRepository) UpdateRecipientStatus(ctx context.Context, campaignID uuid.UUID, userID string, status domain.CampaignRecipientStatus) error {
    return r.db.WithContext(ctx).
        Model(&domain.CampaignRecipient{}).
        Where("campaign_id = ? AND user_id = ?", campaignID, userID).
        Updates(map[string]interface{}{
            "status":     status,
            "updated_at": time.Now().UTC(),
        }).Error
}

// IncrementCampaignCounter tăng sent_count hoặc failed_count.
// field phải là "sent_count" hoặc "failed_count".
func (r *campaignRepository) IncrementCampaignCounter(ctx context.Context, campaignID uuid.UUID, field string) error {
    if field != "sent_count" && field != "failed_count" {
        return fmt.Errorf("invalid counter field: %s", field)
    }
    return r.db.WithContext(ctx).
        Model(&domain.Campaign{}).
        Where("id = ?", campaignID).
        Updates(map[string]interface{}{
            field:        gorm.Expr(field+" + 1"),
            "updated_at": time.Now().UTC(),
        }).Error
}

func (r *campaignRepository) GetCampaignStats(ctx context.Context, id uuid.UUID) (*CampaignStats, error) {
    var campaign domain.Campaign
    if err := r.db.WithContext(ctx).First(&campaign, "id = ?", id).Error; err != nil {
        return nil, err
    }
    pending := campaign.TotalRecipients - campaign.LastDispatchedOffset
    if pending < 0 {
        pending = 0
    }
    return &CampaignStats{
        TotalRecipients:      campaign.TotalRecipients,
        LastDispatchedOffset: campaign.LastDispatchedOffset,
        DispatchedCount:      campaign.DispatchedCount,
        SentCount:            campaign.SentCount,
        FailedCount:          campaign.FailedCount,
        PendingCount:         pending,
    }, nil
}
```

---

## 4. DTO

### 4.1 Thêm vào `dto/api.go`

```go
// --- Campaign DTOs ---

type RecipientInput struct {
    UserID    string `json:"user_id"   binding:"required"`
    Recipient string `json:"recipient" binding:"required"`
}

type CreateCampaignRequest struct {
    Title       string                    `json:"title"        binding:"required"`
    Subject     string                    `json:"subject"`
    Content     string                    `json:"content"      binding:"required"`
    Channel     domain.NotificationChannel `json:"channel"     binding:"required"`
    EventType   domain.EventType          `json:"event_type"   binding:"required"`
    ScheduledAt time.Time                 `json:"scheduled_at" binding:"required"`
    Recipients  []RecipientInput          `json:"recipients"   binding:"required,min=1"`
}

type CreateCampaignResponse struct {
    CampaignID      string    `json:"campaign_id"`
    TotalRecipients int       `json:"total_recipients"`
    ScheduledAt     time.Time `json:"scheduled_at"`
    Message         string    `json:"message"`
}

type CampaignStatsResponse struct {
    CampaignID           string                 `json:"campaign_id"`
    Status               domain.CampaignStatus  `json:"status"`
    TotalRecipients      int                    `json:"total_recipients"`
    LastDispatchedOffset int                    `json:"last_dispatched_offset"`
    DispatchedCount      int                    `json:"dispatched_count"`
    SentCount            int                    `json:"sent_count"`
    FailedCount          int                    `json:"failed_count"`
    PendingCount         int                    `json:"pending_count"`
    ProgressPct          float64                `json:"progress_pct"`
}
```

### 4.2 Thêm vào `dto/worker.go`

```go
// CampaignTask là message được campaign dispatcher publish vào RabbitMQ.
// Khác NotificationTask ở chỗ không có NotificationID (chưa insert DB).
type CampaignTask struct {
    CampaignID string                     `json:"campaign_id"`
    UserID     string                     `json:"user_id"`
    EventType  domain.EventType           `json:"event_type"`
    Recipient  string                     `json:"recipient"`
    Channel    domain.NotificationChannel `json:"channel"`
    Subject    string                     `json:"subject"`
    Content    string                     `json:"content"`
}
```

---

## 5. Service

Tạo file `services/notification/internal/service/campaign_service.go`:

```go
package service

import (
    "context"
    "fmt"
    "time"

    "github.com/google/uuid"
    "github.com/tojinguyen/notification/internal/domain"
    "github.com/tojinguyen/notification/internal/dto"
    "github.com/tojinguyen/notification/internal/repository"
    "go.uber.org/zap"
    "backend/pkg/logger"
)

type CampaignService interface {
    CreateCampaign(ctx context.Context, req dto.CreateCampaignRequest) (dto.CreateCampaignResponse, error)
    GetCampaignStats(ctx context.Context, campaignID string) (dto.CampaignStatsResponse, error)
}

type campaignService struct {
    repo repository.CampaignRepository
}

func NewCampaignService(repo repository.CampaignRepository) CampaignService {
    return &campaignService{repo: repo}
}

func (s *campaignService) CreateCampaign(ctx context.Context, req dto.CreateCampaignRequest) (dto.CreateCampaignResponse, error) {
    if !req.ScheduledAt.After(time.Now().UTC()) {
        return dto.CreateCampaignResponse{}, fmt.Errorf("scheduled_at must be in the future")
    }

    campaign := &domain.Campaign{
        Title:       req.Title,
        Subject:     req.Subject,
        Content:     req.Content,
        Channel:     req.Channel,
        EventType:   req.EventType,
        Status:      domain.CampaignStatusPending,
        ScheduledAt: req.ScheduledAt.UTC(),
    }

    recipients := make([]*domain.CampaignRecipient, len(req.Recipients))
    for i, r := range req.Recipients {
        recipients[i] = &domain.CampaignRecipient{
            UserID:    r.UserID,
            Recipient: r.Recipient,
            Status:    domain.CampaignRecipientStatusPending,
        }
    }

    if err := s.repo.CreateCampaignWithRecipients(ctx, campaign, recipients); err != nil {
        return dto.CreateCampaignResponse{}, err
    }

    logger.L().Info("campaign created",
        zap.String("id", campaign.Id.String()),
        zap.Int("recipients", campaign.TotalRecipients),
        zap.Time("scheduled_at", campaign.ScheduledAt),
    )

    return dto.CreateCampaignResponse{
        CampaignID:      campaign.Id.String(),
        TotalRecipients: campaign.TotalRecipients,
        ScheduledAt:     campaign.ScheduledAt,
        Message:         "campaign created successfully",
    }, nil
}

func (s *campaignService) GetCampaignStats(ctx context.Context, campaignID string) (dto.CampaignStatsResponse, error) {
    id, err := uuid.Parse(campaignID)
    if err != nil {
        return dto.CampaignStatsResponse{}, fmt.Errorf("invalid campaign id: %w", err)
    }

    campaign, err := s.repo.GetCampaignByID(ctx, id)
    if err != nil {
        return dto.CampaignStatsResponse{}, err
    }

    stats, err := s.repo.GetCampaignStats(ctx, id)
    if err != nil {
        return dto.CampaignStatsResponse{}, err
    }

    var progressPct float64
    if stats.TotalRecipients > 0 {
        progressPct = float64(stats.SentCount+stats.FailedCount) / float64(stats.TotalRecipients) * 100
    }

    return dto.CampaignStatsResponse{
        CampaignID:           campaign.Id.String(),
        Status:               campaign.Status,
        TotalRecipients:      stats.TotalRecipients,
        LastDispatchedOffset: stats.LastDispatchedOffset,
        DispatchedCount:      stats.DispatchedCount,
        SentCount:            stats.SentCount,
        FailedCount:          stats.FailedCount,
        PendingCount:         stats.PendingCount,
        ProgressPct:          progressPct,
    }, nil
}
```

---

## 6. Handler

Thêm vào `services/notification/internal/handler/handler.go`:

```go
// --- CampaignHandler ---

type CampaignHandler struct {
    svc service.CampaignService
}

func NewCampaignHandler(svc service.CampaignService) *CampaignHandler {
    return &CampaignHandler{svc: svc}
}

// CreateCampaign godoc
// @Summary      Create a mass notification campaign
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        body body dto.CreateCampaignRequest true "Campaign request"
// @Success      201 {object} response.StandardResponse{data=dto.CreateCampaignResponse}
// @Failure      400 {object} response.StandardResponse{error=object{code=int,message=string}}
// @Router       /admin/campaigns [post]
func (h *CampaignHandler) CreateCampaign(c *gin.Context) {
    var req dto.CreateCampaignRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        response.Error(c.Writer, c.Request, err)
        return
    }

    resp, err := h.svc.CreateCampaign(c.Request.Context(), req)
    if err != nil {
        response.Error(c.Writer, c.Request, err)
        return
    }

    response.OK(c.Writer, resp)
}

// GetCampaignStats godoc
// @Summary      Get campaign delivery stats
// @Tags         admin
// @Produce      json
// @Param        id path string true "Campaign ID"
// @Success      200 {object} response.StandardResponse{data=dto.CampaignStatsResponse}
// @Router       /admin/campaigns/{id}/stats [get]
func (h *CampaignHandler) GetCampaignStats(c *gin.Context) {
    campaignID := c.Param("id")
    if campaignID == "" {
        response.Error(c.Writer, c.Request, errors.New("campaign id is required"))
        return
    }

    resp, err := h.svc.GetCampaignStats(c.Request.Context(), campaignID)
    if err != nil {
        response.Error(c.Writer, c.Request, err)
        return
    }

    response.OK(c.Writer, resp)
}
```

---

## 7. Route

Sửa `services/notification/internal/route/route.go` — thêm tham số `campaignHandler` và nhóm admin:

```go
func RegisterRoutes(
    r *gin.Engine,
    notificationHandler *handler.NotificationHandler,
    preferenceHandler   *handler.PreferenceHandler,
    scheduleHandler     *handler.ScheduleHandler,
    campaignHandler     *handler.CampaignHandler,  // thêm mới
    limiter             *ratelimit.RateLimiter,
    rlCfg               notificationConfig.RateLimitConfig,
) {
    // ... routes hiện có ...

    admin := r.Group("/admin")
    admin.Use(limiter.GinMiddleware(ratelimit.ByIP))
    {
        campaigns := admin.Group("/campaigns")
        {
            campaigns.POST("",          campaignHandler.CreateCampaign)
            campaigns.GET("/:id/stats", campaignHandler.GetCampaignStats)
        }
    }
}
```

> **Lưu ý:** Trong production, nhóm `/admin` cần middleware xác thực riêng (API key hoặc internal service token). Trong scope này có thể để trống hoặc dùng middleware tạm.

---

## 8. Worker — Campaign Dispatcher

Tạo file `services/notification/internal/worker/campaign_worker.go`:

```go
package worker

import (
    "context"
    "time"

    "backend/pkg/broker"
    "backend/pkg/logger"

    "github.com/tojinguyen/notification/internal/config"
    "github.com/tojinguyen/notification/internal/domain"
    "github.com/tojinguyen/notification/internal/dto"
    "github.com/tojinguyen/notification/internal/repository"
    "go.uber.org/zap"
)

const campaignEmailRoutingKey = "campaign.email"

type CampaignWorker struct {
    repo   repository.CampaignRepository
    broker broker.Broker
    cfg    *config.Config
}

func NewCampaignWorker(repo repository.CampaignRepository, broker broker.Broker, cfg *config.Config) *CampaignWorker {
    return &CampaignWorker{repo: repo, broker: broker, cfg: cfg}
}

func (w *CampaignWorker) Start(ctx context.Context) {
    log := logger.L()
    log.Info("Campaign dispatcher started")

    ticker := time.NewTicker(time.Duration(w.cfg.Worker.Interval) * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            log.Info("Campaign dispatcher stopping")
            return
        case <-ticker.C:
            w.dispatchPendingCampaigns(ctx)
        }
    }
}

func (w *CampaignWorker) dispatchPendingCampaigns(ctx context.Context) {
    log := logger.L()

    // Chỉ claim 1 campaign mỗi tick để tránh dispatcher bị overload.
    // Nếu cần xử lý nhiều campaign song song, có thể chạy nhiều pod worker-campaign.
    campaign, err := w.repo.ClaimPendingCampaign(ctx)
    if err != nil {
        log.Error("failed to claim pending campaign", zap.Error(err))
        return
    }
    if campaign == nil {
        return // không có gì để xử lý
    }

    log.Info("dispatching campaign",
        zap.String("campaign_id", campaign.Id.String()),
        zap.Int("total_recipients", campaign.TotalRecipients),
        zap.Int("resume_offset", campaign.LastDispatchedOffset),
    )

    w.dispatchCampaign(ctx, campaign)
}

func (w *CampaignWorker) dispatchCampaign(ctx context.Context, campaign *domain.Campaign) {
    log := logger.L()
    batchSize := w.cfg.Worker.CampaignBatchSize
    offset := campaign.LastDispatchedOffset // resume point nếu crash trước đó

    for {
        if ctx.Err() != nil {
            // Context bị cancel (shutdown signal), dừng lại và giữ nguyên status
            // để lần sau pod mới sẽ resume từ last_dispatched_offset
            log.Info("context cancelled, pausing campaign dispatch",
                zap.String("campaign_id", campaign.Id.String()),
                zap.Int("offset", offset),
            )
            return
        }

        recipients, err := w.repo.GetRecipientsBatch(ctx, campaign.Id, offset, batchSize)
        if err != nil {
            log.Error("failed to fetch recipients batch",
                zap.String("campaign_id", campaign.Id.String()),
                zap.Error(err),
            )
            _ = w.repo.MarkCampaignStatus(ctx, campaign.Id, domain.CampaignStatusFailed)
            return
        }

        if len(recipients) == 0 {
            // Hết recipients → campaign hoàn thành
            _ = w.repo.MarkCampaignStatus(ctx, campaign.Id, domain.CampaignStatusCompleted)
            log.Info("campaign dispatch completed",
                zap.String("campaign_id", campaign.Id.String()),
                zap.Int("dispatched", campaign.LastDispatchedOffset+offset),
            )
            return
        }

        if err := w.publishBatch(ctx, campaign, recipients); err != nil {
            log.Error("failed to publish batch, stopping dispatch",
                zap.String("campaign_id", campaign.Id.String()),
                zap.Int("offset", offset),
                zap.Error(err),
            )
            _ = w.repo.MarkCampaignStatus(ctx, campaign.Id, domain.CampaignStatusFailed)
            return
        }

        // CHECKPOINT: ghi lại tiến độ ngay sau khi publish thành công
        // Nếu crash SAU đây → một số message đã vào queue nhưng chưa checkpoint
        // → duplicates có thể xảy ra. Acceptable trade-off (at-most-once vs at-least-once).
        if err := w.repo.CheckpointDispatch(ctx, campaign.Id, len(recipients)); err != nil {
            log.Error("failed to checkpoint dispatch progress",
                zap.String("campaign_id", campaign.Id.String()),
                zap.Int("batch_size", len(recipients)),
                zap.Error(err),
            )
            // Không dừng — vẫn tiếp tục. Worst case: một số recipients bị dispatch 2 lần
            // khi resume. Với at-least-once delivery, đây là acceptable.
        }

        offset += len(recipients)

        log.Info("batch dispatched",
            zap.String("campaign_id", campaign.Id.String()),
            zap.Int("offset", offset),
            zap.Int("total", campaign.TotalRecipients),
        )
    }
}

func (w *CampaignWorker) publishBatch(ctx context.Context, campaign *domain.Campaign, recipients []*domain.CampaignRecipient) error {
    for _, r := range recipients {
        task := dto.CampaignTask{
            CampaignID: campaign.Id.String(),
            UserID:     r.UserID,
            EventType:  campaign.EventType,
            Recipient:  r.Recipient,
            Channel:    campaign.Channel,
            Subject:    campaign.Subject,
            Content:    campaign.Content,
        }
        if err := w.broker.Publish(ctx, w.cfg.Queue.Exchange, campaignEmailRoutingKey, task); err != nil {
            return err
        }
    }
    return nil
}
```

**Back-pressure — 3 lớp bảo vệ**

Nếu không có kiểm soát, dispatcher có thể publish hết 2M message vào queue chỉ trong vài phút, trong khi worker-email cần hàng chục tiếng để drain. RabbitMQ khi đó phải page sang disk, memory alarm trigger, producer bị block. Giải quyết bằng 3 lớp:

**Lớp 1 — Consumer prefetch (worker-email):** Worker chỉ nhận tối đa N message chưa ACK. RabbitMQ không đẩy thêm cho đến khi worker ACK xong. Cấu hình trong `pkg/broker` khi `QueueSubscribe` khai báo queue:

```go
ch.Qos(
    cfg.Worker.CampaignPrefetch, // ví dụ: 20
    0,
    false,
)
```

**Lớp 2 — Publisher confirms (dispatcher):** Thay vì publish fire-and-forget, dispatcher chờ broker confirm sau mỗi batch. Khi broker bận (memory cao, đang page to disk), confirm chậm lại → dispatcher tự động chậm theo. Sửa `publishBatch` để có retry khi NACK:

```go
func (w *CampaignWorker) publishBatch(ctx context.Context, campaign *domain.Campaign, recipients []*domain.CampaignRecipient) error {
    const maxRetries = 5
    for attempt := range maxRetries {
        err := w.tryPublishBatch(ctx, campaign, recipients)
        if err == nil {
            return nil
        }
        if errors.Is(err, broker.ErrQueueFull) {
            // Queue đang đầy, chờ consumer drain bớt rồi thử lại
            w.logger.Warn("queue full, backing off",
                zap.Int("attempt", attempt+1),
                zap.String("campaign_id", campaign.Id.String()),
            )
            select {
            case <-time.After(time.Duration(attempt+1) * 5 * time.Second):
            case <-ctx.Done():
                return ctx.Err()
            }
            continue
        }
        return err // lỗi khác không retry
    }
    return fmt.Errorf("failed to publish batch after %d retries", maxRetries)
}
```

**Lớp 3 — `x-max-length` trên queue (hard cap):** Khai báo queue `campaign.email` với giới hạn cứng. Khi đầy, broker trả NACK — dispatcher nhận được và thực hiện backoff ở lớp 2. Thêm vào bước khai báo queue trong `pkg/broker` hoặc migration script RabbitMQ:

```go
args := amqp.Table{
    "x-max-length": 50000,         // tối đa 50k message pending
    "x-overflow":   "reject-publish", // NACK thay vì drop silently
}
ch.QueueDeclare("campaign.email", true, false, false, false, args)
```

**Kết quả:** Dispatcher chỉ push nhanh bằng tốc độ mà toàn bộ pipeline có thể hấp thụ được.

```
Dispatcher ──► RabbitMQ [max 50k] ──► Email Workers (20 pod, prefetch=20)
    │               │
    │  NACK (full)  │
    └───── backoff 5s, retry ──────────┘
```

---

## 9. EmailWorker — Xử lý Campaign Messages

Sửa `services/notification/internal/worker/email_worker.go` để subscribe thêm queue campaign:

### 9.1 Thêm CampaignRepository dependency

```go
type EmailWorker struct {
    repo         repository.NotificationRepository
    campaignRepo repository.CampaignRepository  // thêm mới
    broker       broker.Broker
    cfg          *config.Config
    circuitBreaker *CircuitBreaker
}

func NewEmailWorker(
    repo         repository.NotificationRepository,
    campaignRepo repository.CampaignRepository,  // thêm mới
    broker       broker.Broker,
    cfg          *config.Config,
) *EmailWorker {
    return &EmailWorker{
        repo:         repo,
        campaignRepo: campaignRepo,
        broker:       broker,
        cfg:          cfg,
        circuitBreaker: NewCircuitBreaker(
            cfg.Worker.CircuitBreakerMaxFailures,
            cfg.Worker.CircuitBreakerOpenDuration,
        ),
    }
}
```

### 9.2 Sửa `Start()` để subscribe cả 2 queue

```go
func (w *EmailWorker) Start(ctx context.Context) {
    log := logger.L()
    log.Info("Email worker started")

    // Queue gốc cho regular notifications
    go func() {
        if err := w.broker.QueueSubscribe(ctx, "email", w.cfg.Queue.Exchange, string(domain.ChannelEmail), w.HandleMessage); err != nil {
            log.Error("failed to subscribe to email queue", zap.Error(err))
        }
    }()

    // Queue mới cho campaign notifications
    go func() {
        if err := w.broker.QueueSubscribe(ctx, "campaign.email", w.cfg.Queue.Exchange, campaignEmailRoutingKey, w.HandleCampaignMessage); err != nil {
            log.Error("failed to subscribe to campaign.email queue", zap.Error(err))
        }
    }()

    <-ctx.Done()
    log.Info("Email worker stopping")
}
```

### 9.3 Thêm `HandleCampaignMessage`

```go
func (w *EmailWorker) HandleCampaignMessage(ctx context.Context, body []byte) error {
    log := logger.L()
    var task dto.CampaignTask
    if err := json.Unmarshal(body, &task); err != nil {
        log.Error("failed to unmarshal campaign task", zap.Error(err))
        return err
    }

    log.Info("sending campaign email",
        zap.String("campaign_id", task.CampaignID),
        zap.String("user_id", task.UserID),
        zap.String("recipient", task.Recipient),
    )

    err := w.sendEmail("campaign:"+task.CampaignID+":"+task.UserID, task.Recipient, task.Subject, task.Content)
    if errors.Is(err, ErrCircuitBreakerOpen) {
        log.Warn("circuit breaker open, NACK campaign message",
            zap.String("campaign_id", task.CampaignID))
        return err
    }

    campaignID, _ := uuid.Parse(task.CampaignID)

    if err != nil {
        log.Error("failed to send campaign email",
            zap.String("campaign_id", task.CampaignID),
            zap.Error(err),
        )
        // Insert audit record với status failed
        w.insertCampaignAudit(ctx, task, domain.NotificationStatusFailed, err.Error())
        _ = w.campaignRepo.UpdateRecipientStatus(ctx, campaignID, task.UserID, domain.CampaignRecipientStatusFailed)
        _ = w.campaignRepo.IncrementCampaignCounter(ctx, campaignID, "failed_count")
        return err
    }

    now := time.Now().UTC()
    // Insert audit record SAU KHI gửi thành công (không phải trước)
    w.insertCampaignAudit(ctx, task, domain.NotificationStatusSent, "")
    _ = w.campaignRepo.UpdateRecipientStatus(ctx, campaignID, task.UserID, domain.CampaignRecipientStatusSent)
    _ = w.campaignRepo.IncrementCampaignCounter(ctx, campaignID, "sent_count")

    log.Info("campaign email sent",
        zap.String("campaign_id", task.CampaignID),
        zap.String("user_id", task.UserID),
        zap.Time("sent_at", now),
    )
    return nil
}

func (w *EmailWorker) insertCampaignAudit(ctx context.Context, task dto.CampaignTask, status domain.NotificationStatus, errMsg string) {
    log := logger.L()
    now := time.Now().UTC()
    var sentAt *time.Time
    if status == domain.NotificationStatusSent {
        sentAt = &now
    }
    notification := &domain.Notification{
        EventType: task.EventType,
        UserID:    task.UserID,
        Channel:   task.Channel,
        Recipient: task.Recipient,
        Subject:   task.Subject,
        Content:   task.Content,
        Status:    status,
        SentAt:    sentAt,
        ErrorMessage: errMsg,
        Metadata: fmt.Sprintf(`{"campaign_id":"%s"}`, task.CampaignID),
    }
    if _, err := w.repo.Create(ctx, notification); err != nil {
        log.Error("failed to insert campaign audit notification",
            zap.String("campaign_id", task.CampaignID),
            zap.Error(err),
        )
    }
}
```

> **Tại sao insert AFTER thay vì BEFORE?** Nếu insert trước → SMTP timeout → record ở trạng thái pending mãi, phải có retry logic phức tạp. Insert sau → chỉ có audit record khi biết chắc kết quả, đơn giản hơn nhiều. Trade-off: nếu worker crash sau khi gửi nhưng trước khi insert audit, record bị mất. Acceptable cho audit log.

---

## 10. Config

Sửa `services/notification/internal/config/config.go`:

```go
const (
    ModeAPI              = "api"
    ModeWorkerPending    = "worker-pending"
    ModeWorkerEmail      = "worker-email"
    ModeWorkerWebhook    = "worker-webhook"
    ModeWorkerScheduler  = "worker-scheduler"
    ModeWorkerCampaign   = "worker-campaign"   // thêm mới
)

type WorkerConfig struct {
    BatchSize                  int           `mapstructure:"batch_size"`
    Interval                   int           `mapstructure:"interval"`
    MaxRetries                 int           `mapstructure:"max_retries"`
    CircuitBreakerMaxFailures  int           `mapstructure:"circuit_breaker_max_failures"`
    CircuitBreakerOpenDuration time.Duration `mapstructure:"circuit_breaker_open_duration"`
    CampaignBatchSize          int           `mapstructure:"campaign_batch_size"`  // thêm mới
    CampaignPrefetch           int           `mapstructure:"campaign_prefetch"`    // thêm mới
}
```

Thêm vào `.env.example`:

```
WORKER_CAMPAIGN_BATCH_SIZE=1000
WORKER_CAMPAIGN_PREFETCH=20
```

Default hợp lý nếu không set: `campaignBatchSize = 1000`, `campaignPrefetch = 20`.

---

## 11. Wiring trong `cmd/main.go`

Thêm case và hàm mới vào `cmd/main.go`:

```go
// Trong switch cfg.AppMode:
case notificationConfig.ModeWorkerCampaign:
    runCampaignWorker(ctx, cfg, database)

// Trong runAPI: thêm campaign handler vào DI graph
campaignRepo := repository.NewCampaignRepository(database)
campaignSvc  := service.NewCampaignService(campaignRepo)
campaignHandler := handler.NewCampaignHandler(campaignSvc)
// Truyền campaignHandler vào RegisterRoutes (đã cập nhật signature)
route.RegisterRoutes(r, notificationHandler, preferenceHandler, scheduleHandler, campaignHandler, limiter, cfg.RateLimit)

// Hàm mới:
func runCampaignWorker(ctx context.Context, cfg *notificationConfig.Config, database *gorm.DB) {
    log := logger.L()
    brokerClient, err := broker.NewRabbitMQ(cfg.Broker)
    if err != nil {
        log.Panic("failed to connect to broker", zap.Error(err))
    }
    defer brokerClient.Close()

    campaignRepo := repository.NewCampaignRepository(database)
    campaignWorker := worker.NewCampaignWorker(campaignRepo, brokerClient, cfg)

    log.Info("Campaign worker starting")
    campaignWorker.Start(ctx)
}

// Sửa runEmailWorker: thêm campaignRepo
func runEmailWorker(ctx context.Context, cfg *notificationConfig.Config, database *gorm.DB) {
    log := logger.L()
    brokerClient, err := broker.NewRabbitMQ(cfg.Broker)
    if err != nil {
        log.Panic("failed to connect to broker", zap.Error(err))
    }
    defer brokerClient.Close()

    notificationRepo := repository.NewNotificationRepository(database)
    campaignRepo     := repository.NewCampaignRepository(database)  // thêm mới
    emailWorker := worker.NewEmailWorker(notificationRepo, campaignRepo, brokerClient, cfg)

    log.Info("Email worker starting")
    emailWorker.Start(ctx)
}
```

---

## 12. Unit Tests

### 12.1 `campaign_service_test.go`

**File:** `services/notification/internal/service/campaign_service_test.go`

| Test case | Mong đợi |
|-----------|----------|
| `ScheduledAt` trong quá khứ | Trả về validation error |
| `Recipients` rỗng | Binding error từ handler |
| Tạo campaign hợp lệ | `CreateCampaignWithRecipients` được gọi 1 lần, return đúng response |
| `GetCampaignStats` với ID invalid | Trả về error parse UUID |
| `GetCampaignStats` campaign không tồn tại | Trả về not found error |

Mock `CampaignRepository` bằng testify mock giống pattern của các service test hiện có.

### 12.2 `campaign_worker_test.go`

**File:** `services/notification/internal/worker/campaign_worker_test.go`

| Test case | Mong đợi |
|-----------|----------|
| `dispatchCampaign` với 0 recipients | `MarkCampaignStatus(completed)` được gọi |
| `dispatchCampaign` với 2.500 recipients, batchSize=1.000 | `publishBatch` gọi 3 lần, `CheckpointDispatch` gọi 3 lần |
| `publishBatch` fail ở batch đầu | `MarkCampaignStatus(failed)` được gọi |
| Context cancel giữa chừng | Dispatcher dừng, status giữ nguyên `dispatching` |
| Resume từ `last_dispatched_offset=2000` với 3.000 total | Chỉ dispatch 1.000 recipients còn lại |

### 12.3 `email_worker_campaign_test.go`

| Test case | Mong đợi |
|-----------|----------|
| SMTP thành công | `insertCampaignAudit` tạo record status=`sent`, `UpdateRecipientStatus(sent)` |
| SMTP thất bại | `insertCampaignAudit` tạo record status=`failed`, `UpdateRecipientStatus(failed)` |
| Circuit breaker OPEN | Trả về `ErrCircuitBreakerOpen`, không insert audit |

---

## 13. Thứ Tự Implementation

```
1.  Tạo và chạy migration (campaigns + campaign_recipients)
2.  Thêm domain/campaign.go (Campaign, CampaignRecipient, status constants)
3.  Thêm repository/campaign_repository.go (interface + implementation)
4.  Thêm dto/api.go (campaign DTOs)
5.  Thêm dto/worker.go (CampaignTask)
6.  Thêm service/campaign_service.go
7.  Thêm handler/handler.go (CampaignHandler)
8.  Sửa route/route.go (thêm /admin/campaigns)
9.  Thêm worker/campaign_worker.go (dispatcher)
10. Sửa worker/email_worker.go (HandleCampaignMessage, insertCampaignAudit)
11. Thêm ModeWorkerCampaign + CampaignBatchSize vào config
12. Wire hết trong cmd/main.go
13. Viết unit tests
14. Test end-to-end (xem mục 14)
```

---

## 14. Kiểm Tra End-to-End

```bash
# 1. Tạo campaign gửi ngay sau 1 phút
curl -X POST http://localhost:8082/admin/campaigns \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Feature launch campaign",
    "subject": "Khám phá tính năng mới",
    "content": "Chào bạn, chúng tôi vừa ra mắt tính năng AI mới!",
    "channel": "email",
    "event_type": "promotion_campaign",
    "scheduled_at": "2026-05-12T12:01:00Z",
    "recipients": [
      {"user_id": "user1", "recipient": "user1@example.com"},
      {"user_id": "user2", "recipient": "user2@example.com"},
      {"user_id": "user3", "recipient": "user3@example.com"}
    ]
  }'

# 2. Theo dõi tiến độ
curl http://localhost:8082/admin/campaigns/<campaign_id>/stats

# 3. Kiểm tra DB
```

```sql
-- Xem campaign đang dispatching
SELECT id, status, total_recipients, last_dispatched_offset, dispatched_count, sent_count
FROM campaigns;

-- Xem từng recipient
SELECT user_id, recipient, status FROM campaign_recipients WHERE campaign_id = '<id>';

-- Xem audit log notifications được insert bởi email worker
SELECT id, user_id, status, metadata, sent_at
FROM notifications
WHERE metadata LIKE '%campaign_id%'
ORDER BY created_at DESC;
```

```bash
# 4. Xem log dispatcher và email worker
make logs
```

**Kiểm tra crash recovery:**

```bash
# Dừng worker-campaign pod giữa chừng, xem last_dispatched_offset trong DB
# Khởi động lại pod → kiểm tra dispatcher resume từ offset cũ, không gửi lại từ đầu
```

---

## 15. Checklist

- [ ] Migration chạy thành công, cả 2 bảng tồn tại với đúng indexes
- [ ] `CreateCampaignWithRecipients` atomic — nếu insert recipients fail thì campaign cũng không tồn tại
- [ ] `ClaimPendingCampaign` thread-safe (FOR UPDATE SKIP LOCKED) — nhiều pod không claim cùng 1 campaign
- [ ] Dispatcher resume đúng từ `last_dispatched_offset` sau khi restart
- [ ] `CheckpointDispatch` được gọi NGAY SAU publish batch, không phải sau toàn bộ campaign
- [ ] `HandleCampaignMessage` insert audit record AFTER send, không phải BEFORE
- [ ] Circuit breaker OPEN → NACK campaign message (không mất), không insert audit
- [ ] `GetCampaignStats.progress_pct` tính đúng: `(sent + failed) / total * 100`
- [ ] `runEmailWorker` trong `main.go` truyền `campaignRepo` vào `NewEmailWorker`
- [ ] `RegisterRoutes` nhận `campaignHandler` và đăng ký route `/admin/campaigns`
- [ ] Config `WORKER_CAMPAIGN_BATCH_SIZE` và `WORKER_CAMPAIGN_PREFETCH` có default fallback hợp lý
- [ ] Queue `campaign.email` khai báo với `x-max-length=50000` và `x-overflow=reject-publish`
- [ ] `publishBatch` có retry loop với exponential backoff khi nhận `ErrQueueFull`
- [ ] Email worker set prefetch qua `ch.Qos(cfg.Worker.CampaignPrefetch, 0, false)` trước khi subscribe
- [ ] Unit test cover crash-recovery scenario (resume từ offset > 0)

---

## Tóm Tắt Thay Đổi

| Layer | File | Thay đổi |
|-------|------|---------|
| DB | migration mới | Bảng `campaigns` + `campaign_recipients` + indexes |
| Domain | `domain/campaign.go` | `Campaign`, `CampaignRecipient`, status constants |
| Repository | `repository/campaign_repository.go` | Interface + implementation đầy đủ |
| DTO | `dto/api.go` | Campaign request/response DTOs |
| DTO | `dto/worker.go` | `CampaignTask` struct |
| Service | `service/campaign_service.go` | `CreateCampaign`, `GetCampaignStats` |
| Handler | `handler/handler.go` | `CampaignHandler` với 2 endpoints |
| Route | `route/route.go` | Nhóm `/admin/campaigns` |
| Worker | `worker/campaign_worker.go` | Campaign dispatcher (mới hoàn toàn) |
| Worker | `worker/email_worker.go` | Subscribe thêm `campaign.email`, `HandleCampaignMessage` |
| Config | `config/config.go` | `ModeWorkerCampaign`, `CampaignBatchSize` |
| Entry | `cmd/main.go` | Case `worker-campaign`, sửa `runEmailWorker` |
