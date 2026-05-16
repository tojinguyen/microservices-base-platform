package service

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"backend/pkg/logger"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/repository"
	"github.com/tojinguyen/notification/internal/repository/mocks"
)

func TestMain(m *testing.M) {
	_ = logger.Init("notification-service-test", "test")
	os.Exit(m.Run())
}

func newCampaignServiceWithMock() (*campaignService, *mocks.MockCampaignRepository) {
	repo := &mocks.MockCampaignRepository{}
	svc := &campaignService{repo: repo}
	return svc, repo
}

func validCreateRequest() dto.CreateCampaignRequest {
	return dto.CreateCampaignRequest{
		Title:       "Test Campaign",
		Subject:     "Hello",
		Content:     "Campaign content",
		Channel:     domain.ChannelEmail,
		EventType:   domain.EventPromotion,
		ScheduledAt: time.Now().UTC().Add(1 * time.Hour),
		Recipients: []dto.RecipientInput{
			{UserID: "user1", Recipient: "user1@example.com"},
		},
	}
}

func TestCreateCampaign_PastScheduledAt(t *testing.T) {
	svc, _ := newCampaignServiceWithMock()

	req := validCreateRequest()
	req.ScheduledAt = time.Now().UTC().Add(-1 * time.Minute) // in the past

	_, err := svc.CreateCampaign(context.Background(), req)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scheduled_at must be in the future")
}

func TestCreateCampaign_RepoError(t *testing.T) {
	svc, repo := newCampaignServiceWithMock()

	repo.On("CreateCampaignWithRecipients", mock.Anything, mock.Anything, mock.Anything).
		Return(errors.New("db error"))

	_, err := svc.CreateCampaign(context.Background(), validCreateRequest())

	assert.Error(t, err)
	assert.Equal(t, "db error", err.Error())
}

func TestCreateCampaign_Success(t *testing.T) {
	svc, repo := newCampaignServiceWithMock()

	repo.On("CreateCampaignWithRecipients", mock.Anything, mock.AnythingOfType("*domain.Campaign"), mock.Anything).
		Run(func(args mock.Arguments) {
			// simulate repo setting the ID
			c := args.Get(1).(*domain.Campaign)
			c.Id = uuid.New()
			c.TotalRecipients = 1
		}).
		Return(nil)

	req := validCreateRequest()
	resp, err := svc.CreateCampaign(context.Background(), req)

	assert.NoError(t, err)
	assert.Equal(t, 1, resp.TotalRecipients)
	assert.Equal(t, "campaign created successfully", resp.Message)
	repo.AssertNumberOfCalls(t, "CreateCampaignWithRecipients", 1)
}

func TestGetCampaignStats_InvalidUUID(t *testing.T) {
	svc, _ := newCampaignServiceWithMock()

	_, err := svc.GetCampaignStats(context.Background(), "not-a-uuid")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid campaign id")
}

func TestGetCampaignStats_NotFound(t *testing.T) {
	svc, repo := newCampaignServiceWithMock()
	id := uuid.New()

	repo.On("GetCampaignByID", mock.Anything, id).Return(nil, errors.New("record not found"))

	_, err := svc.GetCampaignStats(context.Background(), id.String())

	assert.Error(t, err)
	assert.Equal(t, "record not found", err.Error())
}

func TestGetCampaignStats_Success(t *testing.T) {
	svc, repo := newCampaignServiceWithMock()
	id := uuid.New()

	campaign := &domain.Campaign{
		BaseModel: domain.BaseModel{Id: id},
		Status:    domain.CampaignStatusDispatching,
	}
	stats := &repository.CampaignStats{
		TotalRecipients:      100,
		LastDispatchedOffset: 60,
		DispatchedCount:      60,
		SentCount:            50,
		FailedCount:          5,
		PendingCount:         40,
	}

	repo.On("GetCampaignByID", mock.Anything, id).Return(campaign, nil)
	repo.On("GetCampaignStats", mock.Anything, id).Return(stats, nil)

	resp, err := svc.GetCampaignStats(context.Background(), id.String())

	assert.NoError(t, err)
	assert.Equal(t, id.String(), resp.CampaignID)
	assert.Equal(t, domain.CampaignStatusDispatching, resp.Status)
	assert.InDelta(t, 55.0, resp.ProgressPct, 0.01) // (50+5)/100*100
}
