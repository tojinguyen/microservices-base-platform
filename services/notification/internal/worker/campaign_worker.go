package worker

import (
	"context"
	"errors"
	"fmt"
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

// campaignEmailQueueArgs declares x-max-length so RabbitMQ rejects publishes when the
// queue is full (back-pressure layer 3). Workers must be scaled to drain before the cap
// is sustained; dispatcher backs off on publish errors (back-pressure layer 2).
var campaignEmailQueueArgs = map[string]interface{}{
	"x-max-length": int32(50000),
	"x-overflow":   "reject-publish",
}

type CampaignWorker struct {
	repo   repository.CampaignRepository
	broker broker.Broker
	cfg    *config.Config
}

func NewCampaignWorker(repo repository.CampaignRepository, b broker.Broker, cfg *config.Config) *CampaignWorker {
	return &CampaignWorker{repo: repo, broker: b, cfg: cfg}
}

func (w *CampaignWorker) Start(ctx context.Context) {
	log := logger.L()
	log.Info("Campaign dispatcher started")

	interval := w.cfg.Worker.Interval
	if interval <= 0 {
		interval = 10
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
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

	campaign, err := w.repo.ClaimPendingCampaign(ctx)
	if err != nil {
		log.Error("failed to claim pending campaign", zap.Error(err))
		return
	}
	if campaign == nil {
		return
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
	if batchSize <= 0 {
		batchSize = 1000
	}
	offset := campaign.LastDispatchedOffset // resume from checkpoint after crash

	for {
		if ctx.Err() != nil {
			// Graceful shutdown: leave status as "dispatching" so the next pod can resume.
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
			_ = w.repo.MarkCampaignStatus(ctx, campaign.Id, domain.CampaignStatusCompleted)
			log.Info("campaign dispatch completed",
				zap.String("campaign_id", campaign.Id.String()),
				zap.Int("total_dispatched", offset),
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

		// CHECKPOINT: persist progress immediately after a successful publish.
		// If the process crashes here, the batch has already reached the queue, so
		// on resume those recipients will be dispatched again (at-least-once delivery).
		if err := w.repo.CheckpointDispatch(ctx, campaign.Id, len(recipients)); err != nil {
			log.Error("failed to checkpoint dispatch progress",
				zap.String("campaign_id", campaign.Id.String()),
				zap.Int("batch_size", len(recipients)),
				zap.Error(err),
			)
			// Continue without stopping — worst case: small duplicate window on resume.
		}

		offset += len(recipients)

		log.Info("batch dispatched",
			zap.String("campaign_id", campaign.Id.String()),
			zap.Int("offset", offset),
			zap.Int("total", campaign.TotalRecipients),
		)
	}
}

// publishBatch publishes all recipients in one batch with exponential backoff when the
// queue is full (back-pressure layer 2 — publisher-side retry).
func (w *CampaignWorker) publishBatch(ctx context.Context, campaign *domain.Campaign, recipients []*domain.CampaignRecipient) error {
	const maxRetries = 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		err := w.tryPublishBatch(ctx, campaign, recipients)
		if err == nil {
			return nil
		}
		if errors.Is(err, broker.ErrQueueFull) {
			logger.L().Warn("campaign queue full, backing off",
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
		return err
	}
	return fmt.Errorf("failed to publish batch after %d retries", maxRetries)
}

func (w *CampaignWorker) tryPublishBatch(ctx context.Context, campaign *domain.Campaign, recipients []*domain.CampaignRecipient) error {
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
