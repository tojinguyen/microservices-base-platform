package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/repository"
	"go.uber.org/zap"
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

	targetAudience := req.TargetAudience
	if targetAudience == "" {
		targetAudience = "all_users"
	}

	filterJSON, err := json.Marshal(req.Filter)
	if err != nil {
		return dto.CreateCampaignResponse{}, fmt.Errorf("invalid filter: %w", err)
	}

	campaign := &domain.Campaign{
		Title:          req.Title,
		Subject:        req.Subject,
		Content:        req.Content,
		Channel:        req.Channel,
		EventType:      req.EventType,
		TargetAudience: targetAudience,
		FilterCriteria: string(filterJSON),
		Status:         domain.CampaignStatusPending,
		ScheduledAt:    req.ScheduledAt.UTC(),
	}

	if err := s.repo.CreateCampaign(ctx, campaign); err != nil {
		return dto.CreateCampaignResponse{}, err
	}

	logger.L().Info("campaign created",
		zap.String("id", campaign.Id.String()),
		zap.String("target_audience", targetAudience),
		zap.Time("scheduled_at", campaign.ScheduledAt),
	)

	return dto.CreateCampaignResponse{
		CampaignID:      campaign.Id.String(),
		TotalRecipients: 0, // will be set by the campaign worker before dispatch
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
		LastDispatchedCursor: stats.LastDispatchedCursor,
		DispatchedCount:      stats.DispatchedCount,
		SentCount:            stats.SentCount,
		FailedCount:          stats.FailedCount,
		PendingCount:         stats.PendingCount,
		ProgressPct:          progressPct,
	}, nil
}
