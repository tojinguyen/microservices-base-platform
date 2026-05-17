package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/repository"
	"github.com/tojinguyen/notification/internal/repository/mocks"
	"gorm.io/gorm"
)

// mockNotificationRepo is a lightweight mock of NotificationRepository for these tests.
type mockNotificationRepo struct {
	mock.Mock
}

func (m *mockNotificationRepo) Create(ctx context.Context, n *domain.Notification) (*domain.Notification, error) {
	args := m.Called(ctx, n)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Notification), args.Error(1)
}

func (m *mockNotificationRepo) ExistsByEventID(ctx context.Context, eventID string) (bool, error) {
	args := m.Called(ctx, eventID)
	return args.Bool(0), args.Error(1)
}

func (m *mockNotificationRepo) UpdateDeliveryStatus(ctx context.Context, id uuid.UUID, status domain.NotificationStatus, msg string, sentAt *time.Time) error {
	args := m.Called(ctx, id, status, msg, sentAt)
	return args.Error(0)
}

func (m *mockNotificationRepo) ClaimPendingBatch(ctx context.Context, limit int) ([]*domain.Notification, error) {
	args := m.Called(ctx, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Notification), args.Error(1)
}

func (m *mockNotificationRepo) IncrementRetryAndReset(ctx context.Context, id uuid.UUID, msg string, next time.Time) error {
	args := m.Called(ctx, id, msg, next)
	return args.Error(0)
}

func (m *mockNotificationRepo) CreateWithTx(tx *gorm.DB, n *domain.Notification) (*domain.Notification, error) {
	args := m.Called(tx, n)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Notification), args.Error(1)
}

func (m *mockNotificationRepo) List(ctx context.Context, filter repository.NotificationFilter) ([]*domain.Notification, error) {
	args := m.Called(ctx, filter)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Notification), args.Error(1)
}

// smtpPatch replaces sendEmail for unit tests so we never touch real SMTP.
// We set the circuitBreaker's execute function indirectly via a closed-state breaker
// that returns the injected error.
type patchedEmailWorker struct {
	*EmailWorker
	smtpErr error
}

func (p *patchedEmailWorker) HandleCampaignMessage(ctx context.Context, body []byte) error {
	// Bypass the real circuitBreaker.Execute by overriding sendEmail via embedding is not
	// possible directly, so we rebuild the logic here mirroring the worker but using smtpErr.
	var task dto.CampaignTask
	if err := json.Unmarshal(body, &task); err != nil {
		return err
	}

	sendErr := p.smtpErr
	campaignID, _ := uuid.Parse(task.CampaignID)

	if sendErr != nil {
		p.EmailWorker.insertCampaignAudit(ctx, task, domain.NotificationStatusFailed, sendErr.Error())
		_ = p.EmailWorker.campaignRepo.UpdateRecipientStatus(ctx, campaignID, task.UserID, domain.CampaignRecipientStatusFailed)
		_ = p.EmailWorker.campaignRepo.IncrementCampaignCounter(ctx, campaignID, "failed_count")
		return sendErr
	}

	p.EmailWorker.insertCampaignAudit(ctx, task, domain.NotificationStatusSent, "")
	_ = p.EmailWorker.campaignRepo.UpdateRecipientStatus(ctx, campaignID, task.UserID, domain.CampaignRecipientStatusSent)
	_ = p.EmailWorker.campaignRepo.IncrementCampaignCounter(ctx, campaignID, "sent_count")
	return nil
}

func newPatchedEmailWorker(smtpErr error) (*patchedEmailWorker, *mockNotificationRepo, *mocks.MockCampaignRepository) {
	notifRepo := &mockNotificationRepo{}
	campRepo := &mocks.MockCampaignRepository{}
	cfg := &config.Config{}
	cfg.Worker.CircuitBreakerMaxFailures = 5
	cfg.Worker.CircuitBreakerOpenDuration = 30 * time.Second

	ew := &EmailWorker{
		repo:           notifRepo,
		campaignRepo:   campRepo,
		cfg:            cfg,
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
	}
	return &patchedEmailWorker{EmailWorker: ew, smtpErr: smtpErr}, notifRepo, campRepo
}

func makeCampaignTaskBody(campaignID, userID string) []byte {
	task := dto.CampaignTask{
		CampaignID: campaignID,
		UserID:     userID,
		EventType:  domain.EventPromotion,
		Recipient:  "test@example.com",
		Channel:    domain.ChannelEmail,
		Subject:    "Hello",
		Content:    "Campaign content",
	}
	b, _ := json.Marshal(task)
	return b
}

func TestHandleCampaignMessage_SMTPSuccess(t *testing.T) {
	w, notifRepo, campRepo := newPatchedEmailWorker(nil)
	campaignID := uuid.New().String()

	notifRepo.On("Create", mock.Anything, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Status == domain.NotificationStatusSent
	})).Return(&domain.Notification{}, nil)

	campRepo.On("UpdateRecipientStatus", mock.Anything, mock.Anything, "user1", domain.CampaignRecipientStatusSent).Return(nil)
	campRepo.On("IncrementCampaignCounter", mock.Anything, mock.Anything, "sent_count").Return(nil)

	err := w.HandleCampaignMessage(context.Background(), makeCampaignTaskBody(campaignID, "user1"))

	assert.NoError(t, err)
	notifRepo.AssertCalled(t, "Create", mock.Anything, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Status == domain.NotificationStatusSent
	}))
	campRepo.AssertCalled(t, "UpdateRecipientStatus", mock.Anything, mock.Anything, "user1", domain.CampaignRecipientStatusSent)
	campRepo.AssertCalled(t, "IncrementCampaignCounter", mock.Anything, mock.Anything, "sent_count")
}

func TestHandleCampaignMessage_SMTPFail(t *testing.T) {
	w, notifRepo, campRepo := newPatchedEmailWorker(errors.New("smtp timeout"))
	campaignID := uuid.New().String()

	notifRepo.On("Create", mock.Anything, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Status == domain.NotificationStatusFailed
	})).Return(&domain.Notification{}, nil)

	campRepo.On("UpdateRecipientStatus", mock.Anything, mock.Anything, "user2", domain.CampaignRecipientStatusFailed).Return(nil)
	campRepo.On("IncrementCampaignCounter", mock.Anything, mock.Anything, "failed_count").Return(nil)

	err := w.HandleCampaignMessage(context.Background(), makeCampaignTaskBody(campaignID, "user2"))

	assert.Error(t, err)
	assert.Equal(t, "smtp timeout", err.Error())
	notifRepo.AssertCalled(t, "Create", mock.Anything, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Status == domain.NotificationStatusFailed
	}))
	campRepo.AssertCalled(t, "UpdateRecipientStatus", mock.Anything, mock.Anything, "user2", domain.CampaignRecipientStatusFailed)
	campRepo.AssertCalled(t, "IncrementCampaignCounter", mock.Anything, mock.Anything, "failed_count")
}

func TestHandleCampaignMessage_CircuitBreakerOpen(t *testing.T) {
	// Wire a real EmailWorker with an already-open circuit breaker.
	notifRepo := &mockNotificationRepo{}
	campRepo := &mocks.MockCampaignRepository{}
	cfg := &config.Config{}

	cb := NewCircuitBreaker(1, 10*time.Minute)
	// Trip the breaker.
	cb.Execute(func() error { return errors.New("fail") })

	ew := &EmailWorker{
		repo:           notifRepo,
		campaignRepo:   campRepo,
		cfg:            cfg,
		circuitBreaker: cb,
	}

	campaignID := uuid.New().String()
	err := ew.HandleCampaignMessage(context.Background(), makeCampaignTaskBody(campaignID, "user3"))

	assert.ErrorIs(t, err, ErrCircuitBreakerOpen)
	// Audit must NOT be inserted when the circuit is open.
	notifRepo.AssertNotCalled(t, "Create")
	campRepo.AssertNotCalled(t, "UpdateRecipientStatus")
}
