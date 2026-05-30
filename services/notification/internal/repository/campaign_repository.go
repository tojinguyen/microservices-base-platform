package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/tojinguyen/notification/internal/domain"
	"gorm.io/gorm"
)

type CampaignStats struct {
	TotalRecipients      int    `json:"total_recipients"`
	LastDispatchedCursor string `json:"last_dispatched_cursor"`
	DispatchedCount      int    `json:"dispatched_count"`
	SentCount            int    `json:"sent_count"`
	FailedCount          int    `json:"failed_count"`
	PendingCount         int    `json:"pending_count"`
}

type CampaignRepository interface {
	// Admin API
	CreateCampaign(ctx context.Context, campaign *domain.Campaign) error
	GetCampaignByID(ctx context.Context, id uuid.UUID) (*domain.Campaign, error)
	GetCampaignStats(ctx context.Context, id uuid.UUID) (*CampaignStats, error)

	// Dispatcher
	ClaimPendingCampaign(ctx context.Context) (*domain.Campaign, error)
	SetTotalRecipients(ctx context.Context, campaignID uuid.UUID, count int64) error
	CheckpointDispatch(ctx context.Context, campaignID uuid.UUID, cursor string, batchSize int) error
	MarkCampaignStatus(ctx context.Context, campaignID uuid.UUID, status domain.CampaignStatus) error

	// Email worker — inserts a recipient audit record after the email is sent.
	CreateRecipient(ctx context.Context, recipient *domain.CampaignRecipient) error
	IncrementCampaignCounter(ctx context.Context, campaignID uuid.UUID, field string) error
}

type campaignRepository struct {
	db *gorm.DB
}

func NewCampaignRepository(db *gorm.DB) CampaignRepository {
	return &campaignRepository{db: db}
}

func (r *campaignRepository) CreateCampaign(ctx context.Context, campaign *domain.Campaign) error {
	return r.db.WithContext(ctx).Create(campaign).Error
}

func (r *campaignRepository) GetCampaignByID(ctx context.Context, id uuid.UUID) (*domain.Campaign, error) {
	var campaign domain.Campaign
	if err := r.db.WithContext(ctx).First(&campaign, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &campaign, nil
}

func (r *campaignRepository) GetCampaignStats(ctx context.Context, id uuid.UUID) (*CampaignStats, error) {
	var campaign domain.Campaign
	if err := r.db.WithContext(ctx).First(&campaign, "id = ?", id).Error; err != nil {
		return nil, err
	}
	pending := campaign.TotalRecipients - campaign.SentCount - campaign.FailedCount
	if pending < 0 {
		pending = 0
	}
	return &CampaignStats{
		TotalRecipients:      campaign.TotalRecipients,
		LastDispatchedCursor: campaign.LastDispatchedCursor,
		DispatchedCount:      campaign.DispatchedCount,
		SentCount:            campaign.SentCount,
		FailedCount:          campaign.FailedCount,
		PendingCount:         pending,
	}, nil
}

// ClaimPendingCampaign atomically fetches one ready campaign and flips its status to
// "dispatching". Uses SELECT FOR UPDATE SKIP LOCKED so multiple pods never race.
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
			return nil
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
		return nil, nil
	}
	return &campaign, nil
}

func (r *campaignRepository) SetTotalRecipients(ctx context.Context, campaignID uuid.UUID, count int64) error {
	return r.db.WithContext(ctx).
		Model(&domain.Campaign{}).
		Where("id = ?", campaignID).
		Updates(map[string]interface{}{
			"total_recipients": count,
			"updated_at":       time.Now().UTC(),
		}).Error
}

// CheckpointDispatch saves the cursor of the last dispatched user and advances
// dispatched_count after a batch has been successfully published to RabbitMQ.
func (r *campaignRepository) CheckpointDispatch(ctx context.Context, campaignID uuid.UUID, cursor string, batchSize int) error {
	return r.db.WithContext(ctx).
		Model(&domain.Campaign{}).
		Where("id = ?", campaignID).
		Updates(map[string]interface{}{
			"last_dispatched_cursor": cursor,
			"dispatched_count":       gorm.Expr("dispatched_count + ?", batchSize),
			"updated_at":             time.Now().UTC(),
		}).Error
}

func (r *campaignRepository) MarkCampaignStatus(ctx context.Context, campaignID uuid.UUID, status domain.CampaignStatus) error {
	return r.db.WithContext(ctx).
		Model(&domain.Campaign{}).
		Where("id = ?", campaignID).
		Updates(map[string]interface{}{
			"status":     status,
			"updated_at": time.Now().UTC(),
		}).Error
}

// CreateRecipient inserts an audit record after the email worker sends (or fails) a campaign email.
func (r *campaignRepository) CreateRecipient(ctx context.Context, recipient *domain.CampaignRecipient) error {
	return r.db.WithContext(ctx).Create(recipient).Error
}

// IncrementCampaignCounter increments sent_count or failed_count by 1.
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
