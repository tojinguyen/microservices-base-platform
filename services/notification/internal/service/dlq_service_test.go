package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"backend/pkg/broker"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/repository"
)

// --- Mock structures for DLQ Service testing using interface embedding ---

type mockDLQRepository struct {
	repository.DLQRepository
	mock.Mock
}

func (m *mockDLQRepository) Save(ctx context.Context, msg *domain.DLQMessage) error {
	args := m.Called(ctx, msg)
	return args.Error(0)
}

func (m *mockDLQRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.DLQMessage, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.DLQMessage), args.Error(1)
}

func (m *mockDLQRepository) List(ctx context.Context, status string, page, limit int) ([]*domain.DLQMessage, int64, error) {
	args := m.Called(ctx, status, page, limit)
	if args.Get(0) == nil {
		return nil, 0, args.Error(2)
	}
	return args.Get(0).([]*domain.DLQMessage), int64(args.Int(1)), args.Error(2)
}

func (m *mockDLQRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.DLQStatus) error {
	args := m.Called(ctx, id, status)
	return args.Error(0)
}

type mockNotificationRepository struct {
	repository.NotificationRepository
	mock.Mock
}

func (m *mockNotificationRepository) UpdateDeliveryStatus(ctx context.Context, id uuid.UUID, status domain.NotificationStatus, errMsg string, sentAt *time.Time) error {
	args := m.Called(ctx, id, status, errMsg, sentAt)
	return args.Error(0)
}

type mockBroker struct {
	broker.Broker
	mock.Mock
}

func (m *mockBroker) Publish(ctx context.Context, exchange string, routingKey string, payload interface{}) error {
	args := m.Called(ctx, exchange, routingKey, payload)
	return args.Error(0)
}

// --- Test Cases ---

func TestListDLQMessages(t *testing.T) {
	repo := &mockDLQRepository{}
	svc := &dlqService{repo: repo}

	id := uuid.New()
	msg := &domain.DLQMessage{
		BaseModel:    domain.BaseModel{Id: id, CreatedAt: time.Now()},
		QueueName:    "email",
		Payload:      json.RawMessage(`{"notification_id":"test-noti-id"}`),
		ErrorMessage: "SMTP Timeout",
		Status:       domain.DLQStatusPending,
	}

	repo.On("List", mock.Anything, "pending", 1, 10).Return([]*domain.DLQMessage{msg}, 1, nil)

	res, total, err := svc.ListDLQMessages(context.Background(), "pending", 1, 10)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, res, 1)
	assert.Equal(t, id.String(), res[0].ID)
	assert.Equal(t, "email", res[0].QueueName)
	assert.Equal(t, "pending", res[0].Status)
}

func TestReplayDLQMessage_Success(t *testing.T) {
	repo := &mockDLQRepository{}
	notiRepo := &mockNotificationRepository{}
	b := &mockBroker{}
	cfg := &config.Config{}
	cfg.Queue.Exchange = "notification.direct"

	svc := NewDLQService(repo, notiRepo, b, cfg)

	id := uuid.New()
	notiID := uuid.New()
	msg := &domain.DLQMessage{
		BaseModel:      domain.BaseModel{Id: id},
		NotificationID: &notiID,
		QueueName:      "email",
		Payload:        json.RawMessage(`{"notification_id":"` + notiID.String() + `","retry_count":3}`),
		ErrorMessage:   "SMTP Timeout",
		Status:         domain.DLQStatusPending,
	}

	repo.On("GetByID", mock.Anything, id).Return(msg, nil)

	// Validate reset RetryCount to 0 republished
	expectedTask := dto.NotificationTask{
		NotificationID: notiID.String(),
		RetryCount:     0,
	}
	b.On("Publish", mock.Anything, "notification.direct", "email", expectedTask).Return(nil)

	repo.On("UpdateStatus", mock.Anything, id, domain.DLQStatusReplayed).Return(nil)
	notiRepo.On("UpdateDeliveryStatus", mock.Anything, notiID, domain.NotificationStatusPending, "Replayed from DLQ", mock.Anything).Return(nil)

	err := svc.ReplayDLQMessage(context.Background(), id.String())
	assert.NoError(t, err)

	b.AssertExpectations(t)
	repo.AssertExpectations(t)
	notiRepo.AssertExpectations(t)
}
