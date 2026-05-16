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
	TotalRecipients      int `json:"total_recipients"`
	LastDispatchedOffset int `json:"last_dispatched_offset"`
	DispatchedCount      int `json:"dispatched_count"`
	SentCount            int `json:"sent_count"`
	FailedCount          int `json:"failed_count"`
	PendingCount         int `json:"pending_count"`
}

type CampaignRepository interface {
	// Admin API
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

type campaignRepository struct {
	db *gorm.DB
}

func NewCampaignRepository(db *gorm.DB) CampaignRepository {
	return &campaignRepository{db: db}
}

// CreateCampaignWithRecipients writes campaign + recipients atomically and sets total_recipients.
func (r *campaignRepository) CreateCampaignWithRecipients(ctx context.Context, campaign *domain.Campaign, recipients []*domain.CampaignRecipient) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		campaign.TotalRecipients = len(recipients)
		if err := tx.Create(campaign).Error; err != nil {
			return err
		}
		for i := range recipients {
			recipients[i].CampaignID = campaign.Id
		}
		// Batch-insert in chunks to stay under Postgres max-parameters limit.
		return tx.CreateInBatches(recipients, 500).Error
	})
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
			return nil // nothing to dispatch
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

// GetRecipientsBatch returns pending recipients page by offset for the dispatcher.
func (r *campaignRepository) GetRecipientsBatch(ctx context.Context, campaignID uuid.UUID, offset, limit int) ([]*domain.CampaignRecipient, error) {
	var recipients []*domain.CampaignRecipient
	err := r.db.WithContext(ctx).
		Where("campaign_id = ? AND status = 'pending'", campaignID).
		Order("created_at ASC, id ASC").
		Offset(offset).Limit(limit).
		Find(&recipients).Error
	return recipients, err
}

// CheckpointDispatch atomically advances last_dispatched_offset and dispatched_count
// after a batch has been successfully published.
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

func (r *campaignRepository) MarkCampaignStatus(ctx context.Context, campaignID uuid.UUID, status domain.CampaignStatus) error {
	return r.db.WithContext(ctx).
		Model(&domain.Campaign{}).
		Where("id = ?", campaignID).
		Updates(map[string]interface{}{
			"status":     status,
			"updated_at": time.Now().UTC(),
		}).Error
}

// UpdateRecipientStatus is called by the email worker to mark a recipient sent/failed.
func (r *campaignRepository) UpdateRecipientStatus(ctx context.Context, campaignID uuid.UUID, userID string, status domain.CampaignRecipientStatus) error {
	return r.db.WithContext(ctx).
		Model(&domain.CampaignRecipient{}).
		Where("campaign_id = ? AND user_id = ?", campaignID, userID).
		Updates(map[string]interface{}{
			"status":     status,
			"updated_at": time.Now().UTC(),
		}).Error
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
