package service

import (
	"context"
	"fmt"
	"time"

	pkgerrors "backend/pkg/errors"
	pkgredis "backend/pkg/redis"

	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/repository"
)

const (
	prefCacheTTL    = 5 * time.Minute
	prefCachePrefix = "pref:"
)

func prefCacheKey(userID string) string {
	return prefCachePrefix + userID
}

type PreferenceService interface {
	GetPreferences(ctx context.Context, userID string) (dto.GetPreferencesResponse, error)
	UpsertPreferences(ctx context.Context, userID string, req dto.UpsertPreferencesRequest) (dto.UpsertPreferencesResponse, error)
	IsNotificationEnabled(ctx context.Context, userID string, eventType domain.EventType, channel domain.NotificationChannel) (bool, error)
}

type preferenceService struct {
	repo  repository.PreferenceRepository
	cache *pkgredis.Cache
}

func NewPreferenceService(repo repository.PreferenceRepository, cache *pkgredis.Cache) PreferenceService {
	return &preferenceService{repo: repo, cache: cache}
}

func (s *preferenceService) GetPreferences(ctx context.Context, userID string) (dto.GetPreferencesResponse, error) {
	var cached []dto.PreferenceItem
	if err := s.cache.Get(ctx, prefCacheKey(userID), &cached); err == nil {
		return dto.GetPreferencesResponse{UserID: userID, Preferences: cached}, nil
	}

	rows, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return dto.GetPreferencesResponse{}, fmt.Errorf("failed to get preferences: %w", err)
	}

	items := toPreferenceItems(rows)
	_ = s.cache.Set(ctx, prefCacheKey(userID), items, prefCacheTTL)

	return dto.GetPreferencesResponse{UserID: userID, Preferences: items}, nil
}

func (s *preferenceService) UpsertPreferences(ctx context.Context, userID string, req dto.UpsertPreferencesRequest) (dto.UpsertPreferencesResponse, error) {
	for _, item := range req.Preferences {
		if item.EventType != domain.EventPromotion {
			return dto.UpsertPreferencesResponse{}, pkgerrors.BadRequest(
				nil,
				fmt.Sprintf("event_type %q cannot be toggled; only %q is user-configurable", item.EventType, domain.EventPromotion),
			)
		}
	}

	upserted := make([]*domain.NotificationPreference, 0, len(req.Preferences))
	for _, item := range req.Preferences {
		pref := &domain.NotificationPreference{
			UserID:    userID,
			EventType: item.EventType,
			Channel:   item.Channel,
			Enabled:   item.Enabled,
		}
		result, err := s.repo.Upsert(ctx, pref)
		if err != nil {
			return dto.UpsertPreferencesResponse{}, fmt.Errorf("failed to upsert preference: %w", err)
		}
		upserted = append(upserted, result)
	}

	_ = s.cache.Delete(ctx, prefCacheKey(userID))

	items := toPreferenceItems(upserted)
	return dto.UpsertPreferencesResponse{
		UserID:      userID,
		Preferences: items,
		Message:     "preferences updated successfully",
	}, nil
}

func (s *preferenceService) IsNotificationEnabled(ctx context.Context, userID string, eventType domain.EventType, channel domain.NotificationChannel) (bool, error) {
	if eventType != domain.EventPromotion {
		return true, nil
	}

	var cached []dto.PreferenceItem
	if err := s.cache.Get(ctx, prefCacheKey(userID), &cached); err == nil {
		for _, item := range cached {
			if item.EventType == eventType && item.Channel == channel {
				return item.Enabled, nil
			}
		}
		return true, nil
	}

	return s.repo.IsEnabled(ctx, userID, eventType, channel)
}

func toPreferenceItems(rows []*domain.NotificationPreference) []dto.PreferenceItem {
	items := make([]dto.PreferenceItem, len(rows))
	for i, r := range rows {
		items[i] = dto.PreferenceItem{
			ID:        r.Id.String(),
			EventType: r.EventType,
			Channel:   r.Channel,
			Enabled:   r.Enabled,
			UpdatedAt: r.UpdatedAt,
		}
	}
	return items
}
