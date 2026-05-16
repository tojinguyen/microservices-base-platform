package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/repository/mocks"
)

func testCampaignWorker(repo *mocks.MockCampaignRepository, b *mockBroker) *CampaignWorker {
	cfg := &config.Config{}
	cfg.Worker.CampaignBatchSize = 1000
	cfg.Queue.Exchange = "notification.direct"
	return NewCampaignWorker(repo, b, cfg)
}

func makeCampaign(total, offset int) *domain.Campaign {
	return &domain.Campaign{
		BaseModel:            domain.BaseModel{Id: uuid.New()},
		Status:               domain.CampaignStatusDispatching,
		Channel:              domain.ChannelEmail,
		EventType:            domain.EventPromotion,
		TotalRecipients:      total,
		LastDispatchedOffset: offset,
	}
}

func makeRecipients(n int) []*domain.CampaignRecipient {
	r := make([]*domain.CampaignRecipient, n)
	for i := range r {
		r[i] = &domain.CampaignRecipient{
			Id:        uuid.New(),
			UserID:    "user" + string(rune('0'+i)),
			Recipient: "u@example.com",
		}
	}
	return r
}

// TestDispatchCampaign_ZeroRecipients — empty first batch → campaign marked completed.
func TestDispatchCampaign_ZeroRecipients(t *testing.T) {
	repo := &mocks.MockCampaignRepository{}
	b := &mockBroker{}
	w := testCampaignWorker(repo, b)
	campaign := makeCampaign(0, 0)

	repo.On("GetRecipientsBatch", mock.Anything, campaign.Id, 0, 1000).Return([]*domain.CampaignRecipient{}, nil)
	repo.On("MarkCampaignStatus", mock.Anything, campaign.Id, domain.CampaignStatusCompleted).Return(nil)

	w.dispatchCampaign(context.Background(), campaign)

	repo.AssertCalled(t, "MarkCampaignStatus", mock.Anything, campaign.Id, domain.CampaignStatusCompleted)
	b.AssertNotCalled(t, "Publish")
}

// TestDispatchCampaign_ThreeBatches — 2500 recipients with batchSize=1000 → 3 publishes and 3 checkpoints.
func TestDispatchCampaign_ThreeBatches(t *testing.T) {
	repo := &mocks.MockCampaignRepository{}
	b := &mockBroker{}
	w := testCampaignWorker(repo, b)
	campaign := makeCampaign(2500, 0)

	batch1 := makeRecipients(1000)
	batch2 := makeRecipients(1000)
	batch3 := makeRecipients(500)

	repo.On("GetRecipientsBatch", mock.Anything, campaign.Id, 0, 1000).Return(batch1, nil)
	repo.On("GetRecipientsBatch", mock.Anything, campaign.Id, 1000, 1000).Return(batch2, nil)
	repo.On("GetRecipientsBatch", mock.Anything, campaign.Id, 2000, 1000).Return(batch3, nil)
	repo.On("GetRecipientsBatch", mock.Anything, campaign.Id, 2500, 1000).Return([]*domain.CampaignRecipient{}, nil)
	repo.On("CheckpointDispatch", mock.Anything, campaign.Id, mock.AnythingOfType("int")).Return(nil)
	repo.On("MarkCampaignStatus", mock.Anything, campaign.Id, domain.CampaignStatusCompleted).Return(nil)

	b.On("Publish", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	w.dispatchCampaign(context.Background(), campaign)

	repo.AssertNumberOfCalls(t, "CheckpointDispatch", 3)
	repo.AssertCalled(t, "MarkCampaignStatus", mock.Anything, campaign.Id, domain.CampaignStatusCompleted)
}

// TestDispatchCampaign_PublishFail — first batch publish error → campaign marked failed.
func TestDispatchCampaign_PublishFail(t *testing.T) {
	repo := &mocks.MockCampaignRepository{}
	b := &mockBroker{}
	w := testCampaignWorker(repo, b)
	campaign := makeCampaign(100, 0)

	repo.On("GetRecipientsBatch", mock.Anything, campaign.Id, 0, 1000).Return(makeRecipients(5), nil)
	repo.On("MarkCampaignStatus", mock.Anything, campaign.Id, domain.CampaignStatusFailed).Return(nil)

	b.On("Publish", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("broker down"))

	w.dispatchCampaign(context.Background(), campaign)

	repo.AssertCalled(t, "MarkCampaignStatus", mock.Anything, campaign.Id, domain.CampaignStatusFailed)
	repo.AssertNotCalled(t, "CheckpointDispatch")
}

// TestDispatchCampaign_ContextCancel — cancelled context mid-dispatch stops without marking failed.
func TestDispatchCampaign_ContextCancel(t *testing.T) {
	repo := &mocks.MockCampaignRepository{}
	b := &mockBroker{}
	w := testCampaignWorker(repo, b)
	campaign := makeCampaign(5000, 0)

	ctx, cancel := context.WithCancel(context.Background())

	// First batch succeeds, then cancel.
	repo.On("GetRecipientsBatch", mock.Anything, campaign.Id, 0, 1000).
		Run(func(_ mock.Arguments) { cancel() }).
		Return(makeRecipients(1000), nil)
	repo.On("CheckpointDispatch", mock.Anything, campaign.Id, 1000).Return(nil)

	b.On("Publish", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	w.dispatchCampaign(ctx, campaign)

	// Must NOT mark as completed or failed — status stays dispatching for resume.
	repo.AssertNotCalled(t, "MarkCampaignStatus")
}

// TestDispatchCampaign_ResumeFromOffset — last_dispatched_offset=2000, total=3000 → only 1 batch dispatched.
func TestDispatchCampaign_ResumeFromOffset(t *testing.T) {
	repo := &mocks.MockCampaignRepository{}
	b := &mockBroker{}
	w := testCampaignWorker(repo, b)
	campaign := makeCampaign(3000, 2000) // resume from offset 2000

	remaining := makeRecipients(1000)
	repo.On("GetRecipientsBatch", mock.Anything, campaign.Id, 2000, 1000).Return(remaining, nil)
	repo.On("GetRecipientsBatch", mock.Anything, campaign.Id, 3000, 1000).Return([]*domain.CampaignRecipient{}, nil)
	repo.On("CheckpointDispatch", mock.Anything, campaign.Id, 1000).Return(nil)
	repo.On("MarkCampaignStatus", mock.Anything, campaign.Id, domain.CampaignStatusCompleted).Return(nil)

	b.On("Publish", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	w.dispatchCampaign(context.Background(), campaign)

	repo.AssertNumberOfCalls(t, "CheckpointDispatch", 1)
	repo.AssertCalled(t, "MarkCampaignStatus", mock.Anything, campaign.Id, domain.CampaignStatusCompleted)
}

// TestStart_StopsOnContextCancel verifies Start() exits when context is cancelled.
func TestStart_StopsOnContextCancel(t *testing.T) {
	repo := &mocks.MockCampaignRepository{}
	b := &mockBroker{}
	cfg := &config.Config{}
	cfg.Worker.Interval = 1
	cfg.Queue.Exchange = "notification.direct"
	w := NewCampaignWorker(repo, b, cfg)

	repo.On("ClaimPendingCampaign", mock.Anything).Return(nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		w.Start(ctx)
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Start() did not stop after context cancellation")
	}
}
