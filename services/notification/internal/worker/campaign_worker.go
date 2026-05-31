package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"backend/pkg/broker"
	"backend/pkg/logger"

	"github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
	notifgrpc "github.com/tojinguyen/notification/internal/grpc"
	"github.com/tojinguyen/notification/internal/metrics"
	"github.com/tojinguyen/notification/internal/repository"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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

// userFilter mirrors the JSON stored in campaign.filter_criteria.
type userFilter struct {
	Role string `json:"role"`
}

type CampaignWorker struct {
	repo           repository.CampaignRepository
	broker         broker.Broker
	cfg            *config.Config
	identityClient notifgrpc.IdentityClient
}

func NewCampaignWorker(repo repository.CampaignRepository, b broker.Broker, cfg *config.Config, identityClient notifgrpc.IdentityClient) *CampaignWorker {
	return &CampaignWorker{
		repo:           repo,
		broker:         b,
		cfg:            cfg,
		identityClient: identityClient,
	}
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
		zap.String("resume_cursor", campaign.LastDispatchedCursor),
	)

	w.dispatchCampaign(ctx, campaign)
}

func (w *CampaignWorker) dispatchCampaign(ctx context.Context, campaign *domain.Campaign) {
	dispatchStart := time.Now()
	defer func() {
		metrics.CampaignDispatchDuration.Observe(time.Since(dispatchStart).Seconds())
	}()

	tracer := otel.Tracer("notification-service")
	ctx, span := tracer.Start(ctx, "campaign.dispatch")
	defer span.End()
	span.SetAttributes(attribute.String("campaign.id", campaign.Id.String()))

	log := logger.L()

	// Deserialize filter stored at campaign creation time.
	var filter userFilter
	if campaign.FilterCriteria != "" && campaign.FilterCriteria != "{}" {
		if err := json.Unmarshal([]byte(campaign.FilterCriteria), &filter); err != nil {
			log.Error("invalid filter_criteria JSON", zap.String("campaign_id", campaign.Id.String()), zap.Error(err))
		}
	}

	// Set total_recipients once before starting, so the stats endpoint reports correctly.
	total, err := w.identityClient.CountUsers(ctx, filter.Role)
	if err != nil {
		log.Error("failed to count users for campaign", zap.String("campaign_id", campaign.Id.String()), zap.Error(err))
		_ = w.repo.MarkCampaignStatus(ctx, campaign.Id, domain.CampaignStatusFailed)
		return
	}
	if err := w.repo.SetTotalRecipients(ctx, campaign.Id, total); err != nil {
		log.Warn("failed to set total_recipients", zap.String("campaign_id", campaign.Id.String()), zap.Error(err))
	}

	batchSize := w.cfg.Worker.CampaignBatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}
	cursor := campaign.LastDispatchedCursor

	span.SetAttributes(attribute.Int64("campaign.total_recipients", total))
	log.Info("campaign dispatch starting",
		zap.String("campaign_id", campaign.Id.String()),
		zap.Int64("total_recipients", total),
		zap.String("resume_cursor", cursor),
	)

	var batch []*notifgrpc.UserRecord
	var streamErr error
	totalDispatched := 0

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	streamErr = w.identityClient.StreamUsers(streamCtx, filter.Role, cursor, int32(batchSize), func(u *notifgrpc.UserRecord) error {
		if streamCtx.Err() != nil {
			return streamCtx.Err()
		}

		batch = append(batch, u)
		cursor = u.Id

		if len(batch) < batchSize {
			return nil
		}

		// Flush full batch.
		batchLen := len(batch)
		if err := w.publishBatch(streamCtx, campaign, batch); err != nil {
			return err
		}
		if err := w.repo.CheckpointDispatch(streamCtx, campaign.Id, cursor, batchLen); err != nil {
			log.Warn("failed to checkpoint dispatch", zap.String("campaign_id", campaign.Id.String()), zap.Error(err))
		}
		metrics.CampaignBatchesDispatched.Inc()
		metrics.CampaignUsersDispatched.Add(float64(batchLen))
		totalDispatched += batchLen
		log.Info("batch dispatched",
			zap.String("campaign_id", campaign.Id.String()),
			zap.String("cursor", cursor),
			zap.Int("total_dispatched", totalDispatched),
		)
		batch = batch[:0]
		return nil
	})

	// Flush remaining partial batch.
	if streamErr == nil && len(batch) > 0 {
		finalLen := len(batch)
		if err := w.publishBatch(ctx, campaign, batch); err != nil {
			streamErr = err
		} else {
			if err := w.repo.CheckpointDispatch(ctx, campaign.Id, cursor, finalLen); err != nil {
				log.Warn("failed to checkpoint final batch", zap.String("campaign_id", campaign.Id.String()), zap.Error(err))
			}
			metrics.CampaignBatchesDispatched.Inc()
			metrics.CampaignUsersDispatched.Add(float64(finalLen))
			totalDispatched += finalLen
		}
	}

	if streamErr != nil {
		if errors.Is(streamErr, context.Canceled) {
			// Graceful shutdown: leave status as "dispatching" so the next pod can resume.
			log.Info("context cancelled, pausing campaign dispatch", zap.String("campaign_id", campaign.Id.String()))
			return
		}
		log.Error("campaign dispatch failed", zap.String("campaign_id", campaign.Id.String()), zap.Error(streamErr))
		_ = w.repo.MarkCampaignStatus(ctx, campaign.Id, domain.CampaignStatusFailed)
		return
	}

	_ = w.repo.MarkCampaignStatus(ctx, campaign.Id, domain.CampaignStatusCompleted)
	log.Info("campaign dispatch completed",
		zap.String("campaign_id", campaign.Id.String()),
		zap.Int("total_dispatched", totalDispatched),
	)
}

// publishBatch publishes all users in one batch with exponential backoff when the
// queue is full (back-pressure layer 2 — publisher-side retry).
func (w *CampaignWorker) publishBatch(ctx context.Context, campaign *domain.Campaign, users []*notifgrpc.UserRecord) error {
	const maxRetries = 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		err := w.tryPublishBatch(ctx, campaign, users)
		if err == nil {
			return nil
		}
		if errors.Is(err, broker.ErrQueueFull) {
			metrics.CampaignBackoffs.Inc()
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

func (w *CampaignWorker) tryPublishBatch(ctx context.Context, campaign *domain.Campaign, users []*notifgrpc.UserRecord) error {
	for _, u := range users {
		task := dto.CampaignTask{
			CampaignID: campaign.Id.String(),
			UserID:     u.Id,
			EventType:  campaign.EventType,
			Recipient:  u.Email,
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
