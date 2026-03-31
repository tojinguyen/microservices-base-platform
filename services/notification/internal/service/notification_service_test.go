package service

import (
	appErrors "backend/pkg/errors"
	"backend/pkg/logger"
	"context"
	stdErrors "errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
)

func init() {
	_ = logger.Init("notification-service-test")
}

type updateCall struct {
	notificationID uuid.UUID
	status         domain.NotificationStatus
	errorMessage   string
	sentAt         *time.Time
}

type fakeNotificationRepo struct {
	exists      bool
	existsErr   error
	createErr   error
	updateErr   error
	created     []*domain.Notification
	updateCalls []updateCall
}

func (f *fakeNotificationRepo) Create(ctx context.Context, notification *domain.Notification) error {
	if f.createErr != nil {
		return f.createErr
	}

	if notification.Id == uuid.Nil {
		notification.Id = uuid.New()
	}

	copied := *notification
	f.created = append(f.created, &copied)
	return nil
}

func (f *fakeNotificationRepo) ExistsByEventID(ctx context.Context, eventID string) (bool, error) {
	return f.exists, f.existsErr
}

func (f *fakeNotificationRepo) UpdateDeliveryStatus(ctx context.Context, notificationID uuid.UUID, status domain.NotificationStatus, errorMessage string, sentAt *time.Time) error {
	if f.updateErr != nil {
		return f.updateErr
	}

	var copiedSentAt *time.Time
	if sentAt != nil {
		value := *sentAt
		copiedSentAt = &value
	}

	f.updateCalls = append(f.updateCalls, updateCall{
		notificationID: notificationID,
		status:         status,
		errorMessage:   errorMessage,
		sentAt:         copiedSentAt,
	})

	return nil
}

type fakeEmailSender struct {
	err   error
	calls int
	sent  []EmailMessage
}

func (f *fakeEmailSender) Send(ctx context.Context, message EmailMessage) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, message)
	return nil
}

func sampleEvent() dto.NotificationEvent {
	return dto.NotificationEvent{
		EventID:   "evt-1",
		EventType: "order.created",
		Payload: dto.NotificationPayload{
			UserID:  "user-1",
			Email:   "user1@test.com",
			Subject: "Order confirmed",
			Content: "Order #123 has been confirmed",
		},
	}
}

func TestNotificationService_ProcessEvent_Success(t *testing.T) {
	repo := &fakeNotificationRepo{}
	sender := &fakeEmailSender{}
	svc := NewNotificationService(repo, sender)

	err := svc.ProcessEvent(t.Context(), sampleEvent())

	assert.NoError(t, err)
	assert.Len(t, repo.created, 1)
	assert.Len(t, sender.sent, 1)
	assert.Len(t, repo.updateCalls, 1)
	assert.Equal(t, domain.NotificationStatusSent, repo.updateCalls[0].status)
	assert.NotNil(t, repo.updateCalls[0].sentAt)
}

func TestNotificationService_ProcessEvent_SendFailedMarksFailed(t *testing.T) {
	repo := &fakeNotificationRepo{}
	sender := &fakeEmailSender{err: stdErrors.New("smtp unavailable")}
	svc := NewNotificationService(repo, sender)

	err := svc.ProcessEvent(t.Context(), sampleEvent())

	assert.NoError(t, err)
	assert.Len(t, repo.created, 1)
	assert.Equal(t, 1, sender.calls)
	assert.Len(t, repo.updateCalls, 1)
	assert.Equal(t, domain.NotificationStatusFailed, repo.updateCalls[0].status)
	assert.Contains(t, repo.updateCalls[0].errorMessage, "smtp unavailable")
}

func TestNotificationService_ProcessEvent_InvalidPayload(t *testing.T) {
	repo := &fakeNotificationRepo{}
	sender := &fakeEmailSender{}
	svc := NewNotificationService(repo, sender)

	event := sampleEvent()
	event.Payload.Email = ""

	err := svc.ProcessEvent(t.Context(), event)

	assert.Error(t, err)
	assert.Len(t, repo.created, 0)
	assert.Equal(t, 0, sender.calls)

	var appErr *appErrors.AppError
	assert.True(t, stdErrors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
}

func TestNotificationService_ProcessEvent_DuplicateEvent(t *testing.T) {
	repo := &fakeNotificationRepo{exists: true}
	sender := &fakeEmailSender{}
	svc := NewNotificationService(repo, sender)

	err := svc.ProcessEvent(t.Context(), sampleEvent())

	assert.NoError(t, err)
	assert.Len(t, repo.created, 0)
	assert.Equal(t, 0, sender.calls)
	assert.Len(t, repo.updateCalls, 0)
}
