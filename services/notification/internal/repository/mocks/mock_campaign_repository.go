package mocks

import (
	"context"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/repository"
)

type MockCampaignRepository struct {
	mock.Mock
}

func (m *MockCampaignRepository) CreateCampaignWithRecipients(ctx context.Context, campaign *domain.Campaign, recipients []*domain.CampaignRecipient) error {
	args := m.Called(ctx, campaign, recipients)
	return args.Error(0)
}

func (m *MockCampaignRepository) GetCampaignByID(ctx context.Context, id uuid.UUID) (*domain.Campaign, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Campaign), args.Error(1)
}

func (m *MockCampaignRepository) GetCampaignStats(ctx context.Context, id uuid.UUID) (*repository.CampaignStats, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*repository.CampaignStats), args.Error(1)
}

func (m *MockCampaignRepository) ClaimPendingCampaign(ctx context.Context) (*domain.Campaign, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Campaign), args.Error(1)
}

func (m *MockCampaignRepository) GetRecipientsBatch(ctx context.Context, campaignID uuid.UUID, offset, limit int) ([]*domain.CampaignRecipient, error) {
	args := m.Called(ctx, campaignID, offset, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.CampaignRecipient), args.Error(1)
}

func (m *MockCampaignRepository) CheckpointDispatch(ctx context.Context, campaignID uuid.UUID, batchSize int) error {
	args := m.Called(ctx, campaignID, batchSize)
	return args.Error(0)
}

func (m *MockCampaignRepository) MarkCampaignStatus(ctx context.Context, campaignID uuid.UUID, status domain.CampaignStatus) error {
	args := m.Called(ctx, campaignID, status)
	return args.Error(0)
}

func (m *MockCampaignRepository) UpdateRecipientStatus(ctx context.Context, campaignID uuid.UUID, userID string, status domain.CampaignRecipientStatus) error {
	args := m.Called(ctx, campaignID, userID, status)
	return args.Error(0)
}

func (m *MockCampaignRepository) IncrementCampaignCounter(ctx context.Context, campaignID uuid.UUID, field string) error {
	args := m.Called(ctx, campaignID, field)
	return args.Error(0)
}
