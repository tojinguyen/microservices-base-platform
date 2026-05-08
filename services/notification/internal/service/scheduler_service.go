package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"backend/pkg/logger"

	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/repository"
	"go.uber.org/zap"
)

type SchedulerService interface {
	ProcessDueSchedules(ctx context.Context) error
	UpsertSchedule(ctx context.Context, req dto.UpsertScheduleRequest) (dto.ScheduleItem, error)
	GetSchedules(ctx context.Context, userID string) (dto.GetSchedulesResponse, error)
	DeleteSchedule(ctx context.Context, userID string, eventType domain.EventType) error
}

type schedulerService struct {
	scheduleRepo    repository.ScheduleRepository
	notificationSvc NotificationService
}

func NewSchedulerService(scheduleRepo repository.ScheduleRepository, notificationSvc NotificationService) SchedulerService {
	return &schedulerService{
		scheduleRepo:    scheduleRepo,
		notificationSvc: notificationSvc,
	}
}

func (s *schedulerService) ProcessDueSchedules(ctx context.Context) error {
	candidates, err := s.scheduleRepo.FindDueCandidates(ctx)
	if err != nil {
		return fmt.Errorf("find due candidates: %w", err)
	}

	log := logger.L()
	now := time.Now().UTC()

	for _, schedule := range candidates {
		if !isDue(schedule, now) {
			continue
		}

		payload := map[string]interface{}{}
		if schedule.Payload != "" {
			if err := json.Unmarshal([]byte(schedule.Payload), &payload); err != nil {
				log.Warn("failed to unmarshal schedule payload, using empty map",
					zap.String("schedule_id", schedule.Id.String()),
					zap.Error(err),
				)
			}
		}

		_, err := s.notificationSvc.CreateNotification(ctx, dto.SendNotificationRequest{
			UserID:    schedule.UserID,
			EventType: schedule.EventType,
			Payload:   payload,
		})
		if err != nil {
			log.Error("failed to create notification for schedule",
				zap.String("schedule_id", schedule.Id.String()),
				zap.String("user_id", schedule.UserID),
				zap.Error(err),
			)
			continue
		}

		if err := s.scheduleRepo.UpdateLastSentAt(ctx, schedule.Id, now); err != nil {
			log.Error("failed to update last_sent_at",
				zap.String("schedule_id", schedule.Id.String()),
				zap.Error(err),
			)
		}

		log.Info("scheduled notification dispatched",
			zap.String("user_id", schedule.UserID),
			zap.String("event_type", string(schedule.EventType)),
			zap.String("send_time", schedule.SendTime),
			zap.String("timezone", schedule.Timezone),
		)
	}

	return nil
}

func (s *schedulerService) UpsertSchedule(ctx context.Context, req dto.UpsertScheduleRequest) (dto.ScheduleItem, error) {
	if err := validateSendTime(req.SendTime); err != nil {
		return dto.ScheduleItem{}, err
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil {
		return dto.ScheduleItem{}, fmt.Errorf("invalid timezone %q: %w", req.Timezone, err)
	}

	var payloadStr string
	if req.Payload != nil {
		b, err := json.Marshal(req.Payload)
		if err != nil {
			return dto.ScheduleItem{}, fmt.Errorf("failed to marshal payload: %w", err)
		}
		payloadStr = string(b)
	}

	schedule := &domain.UserNotificationSchedule{
		UserID:    req.UserID,
		EventType: req.EventType,
		SendTime:  req.SendTime,
		Timezone:  req.Timezone,
		Enabled:   req.Enabled,
		Payload:   payloadStr,
	}

	saved, err := s.scheduleRepo.Upsert(ctx, schedule)
	if err != nil {
		return dto.ScheduleItem{}, err
	}

	return toScheduleItem(saved), nil
}

func (s *schedulerService) GetSchedules(ctx context.Context, userID string) (dto.GetSchedulesResponse, error) {
	schedules, err := s.scheduleRepo.GetByUserID(ctx, userID)
	if err != nil {
		return dto.GetSchedulesResponse{}, err
	}

	items := make([]dto.ScheduleItem, len(schedules))
	for i, sc := range schedules {
		items[i] = toScheduleItem(sc)
	}

	return dto.GetSchedulesResponse{UserID: userID, Schedules: items}, nil
}

func (s *schedulerService) DeleteSchedule(ctx context.Context, userID string, eventType domain.EventType) error {
	return s.scheduleRepo.Delete(ctx, userID, eventType)
}

// isDue returns true if the schedule's send_time has passed today in the user's timezone
// and the schedule hasn't been sent today yet.
func isDue(s *domain.UserNotificationSchedule, nowUTC time.Time) bool {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		loc = time.UTC
	}

	nowLocal := nowUTC.In(loc)

	parts := strings.SplitN(s.SendTime, ":", 2)
	if len(parts) != 2 {
		return false
	}
	hour, err1 := strconv.Atoi(parts[0])
	min, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}

	sendMoment := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), hour, min, 0, 0, loc)

	// Time has not arrived yet today.
	if nowLocal.Before(sendMoment) {
		return false
	}

	// Already sent today in the user's timezone.
	if s.LastSentAt != nil {
		lastLocal := s.LastSentAt.In(loc)
		sameDay := lastLocal.Year() == nowLocal.Year() &&
			lastLocal.Month() == nowLocal.Month() &&
			lastLocal.Day() == nowLocal.Day()
		if sameDay {
			return false
		}
	}

	return true
}

func validateSendTime(t string) error {
	parts := strings.SplitN(t, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("send_time must be in HH:MM format")
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return fmt.Errorf("send_time must be a valid time in HH:MM format (e.g. 09:00)")
	}
	return nil
}

func toScheduleItem(s *domain.UserNotificationSchedule) dto.ScheduleItem {
	return dto.ScheduleItem{
		ID:          s.Id.String(),
		EventType:   s.EventType,
		SendTime:    s.SendTime,
		Timezone:    s.Timezone,
		Enabled:     s.Enabled,
		LastSentAt:  s.LastSentAt,
		CreatedAt:   s.CreatedAt,
	}
}
