package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"backend/pkg/errors"
	"backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/tojinguyen/upload/internal/config"
	"github.com/tojinguyen/upload/internal/domain"
	"github.com/tojinguyen/upload/internal/dto"
	"github.com/tojinguyen/upload/internal/publisher"
	"github.com/tojinguyen/upload/internal/repository"
	"github.com/tojinguyen/upload/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type VideoService interface {
	InitUpload(ctx context.Context, req dto.InitUploadRequest) (*dto.InitUploadResponse, error)
	CompleteUpload(ctx context.Context, id uuid.UUID) (*dto.CompleteUploadResponse, error)
	AbortUpload(ctx context.Context, id uuid.UUID) (*dto.AbortUploadResponse, error)
	GetVideo(ctx context.Context, id uuid.UUID) (*dto.VideoResponse, error)
	ListVideos(ctx context.Context, req dto.ListVideosRequest) (*dto.ListVideosResponse, error)
}

type videoService struct {
	repo      repository.VideoRepository
	storage   storage.ObjectStorage
	publisher *publisher.EventPublisher
	cfg       *config.Config
}

func NewVideoService(
	repo repository.VideoRepository,
	store storage.ObjectStorage,
	pub *publisher.EventPublisher,
	cfg *config.Config,
) VideoService {
	return &videoService{
		repo:      repo,
		storage:   store,
		publisher: pub,
		cfg:       cfg,
	}
}

func (s *videoService) InitUpload(ctx context.Context, req dto.InitUploadRequest) (*dto.InitUploadResponse, error) {
	if err := s.validateMimeType(req.MimeType); err != nil {
		return nil, err
	}
	if req.SizeBytes > s.cfg.Upload.MaxSizeBytes {
		return nil, errors.BadRequest(nil, fmt.Sprintf("file size exceeds limit of %d bytes", s.cfg.Upload.MaxSizeBytes))
	}

	videoID := uuid.New()
	ext := mimeToExt(req.MimeType)
	now := time.Now()
	objectKey := fmt.Sprintf("videos/%d/%02d/%s%s", now.Year(), now.Month(), videoID.String(), ext)

	ttl := time.Duration(s.cfg.Upload.PresignTTLSeconds) * time.Second
	uploadURL, expiresAt, err := s.storage.PresignPut(ctx, objectKey, req.MimeType, ttl)
	if err != nil {
		logger.L().Error("failed to generate presigned URL", zap.Error(err))
		return nil, errors.InternalServer(err)
	}

	storageURL := s.storage.PublicURL(objectKey)
	video := &domain.Video{
		ID:              videoID,
		Title:           req.Title,
		Description:     req.Description,
		ObjectKey:       objectKey,
		Bucket:          s.cfg.Storage.Bucket,
		MimeType:        req.MimeType,
		Status:          domain.StatusPendingUpload,
		StorageURL:      storageURL,
		UploadExpiresAt: &expiresAt,
	}

	if err := s.repo.Create(ctx, video); err != nil {
		logger.L().Error("failed to create video record", zap.Error(err))
		return nil, errors.InternalServer(err)
	}

	return &dto.InitUploadResponse{
		VideoID:      videoID.String(),
		ObjectKey:    objectKey,
		Bucket:       s.cfg.Storage.Bucket,
		UploadURL:    uploadURL,
		UploadMethod: "PUT",
		ExpiresAt:    expiresAt,
		Status:       string(domain.StatusPendingUpload),
	}, nil
}

func (s *videoService) CompleteUpload(ctx context.Context, id uuid.UUID) (*dto.CompleteUploadResponse, error) {
	video, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.NotFound("video not found")
		}
		return nil, errors.InternalServer(err)
	}

	if video.Status == domain.StatusUploaded {
		return videoToCompleteResponse(video), nil
	}

	sizeBytes, etag, exists, err := s.storage.HeadObject(ctx, video.ObjectKey)
	if err != nil {
		return nil, errors.InternalServer(err)
	}
	if !exists {
		return nil, errors.BadRequest(nil, "object not found in storage")
	}

	now := time.Now()
	video.Status = domain.StatusUploaded
	video.SizeBytes = &sizeBytes
	video.Etag = etag
	video.UploadedAt = &now

	if err := s.repo.Update(ctx, video); err != nil {
		return nil, errors.InternalServer(err)
	}

	s.publishUploadedEvent(ctx, video)

	return videoToCompleteResponse(video), nil
}

func (s *videoService) AbortUpload(ctx context.Context, id uuid.UUID) (*dto.AbortUploadResponse, error) {
	video, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.NotFound("video not found")
		}
		return nil, errors.InternalServer(err)
	}

	if video.Status == domain.StatusAborted {
		return &dto.AbortUploadResponse{VideoID: id.String(), Status: string(video.Status)}, nil
	}

	if err := s.storage.DeleteObject(ctx, video.ObjectKey); err != nil {
		logger.L().Warn("failed to delete object from storage during abort", zap.Error(err), zap.String("key", video.ObjectKey))
	}

	video.Status = domain.StatusAborted
	if err := s.repo.Update(ctx, video); err != nil {
		return nil, errors.InternalServer(err)
	}

	return &dto.AbortUploadResponse{VideoID: id.String(), Status: string(domain.StatusAborted)}, nil
}

func (s *videoService) GetVideo(ctx context.Context, id uuid.UUID) (*dto.VideoResponse, error) {
	video, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.NotFound("video not found")
		}
		return nil, errors.InternalServer(err)
	}
	return videoToResponse(video), nil
}

func (s *videoService) ListVideos(ctx context.Context, req dto.ListVideosRequest) (*dto.ListVideosResponse, error) {
	videos, total, err := s.repo.List(ctx, req.Status, req.Page, req.PageSize)
	if err != nil {
		return nil, errors.InternalServer(err)
	}

	items := make([]dto.VideoResponse, 0, len(videos))
	for i := range videos {
		items = append(items, *videoToResponse(&videos[i]))
	}

	return &dto.ListVideosResponse{
		Videos:   items,
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}, nil
}

func (s *videoService) publishUploadedEvent(ctx context.Context, video *domain.Video) {
	event := dto.VideoUploadedEvent{
		VideoID:    video.ID.String(),
		ObjectKey:  video.ObjectKey,
		Bucket:     video.Bucket,
		MimeType:   video.MimeType,
		SizeBytes:  video.SizeBytes,
		Etag:       video.Etag,
		UploadedAt: video.UploadedAt,
		StorageURL: video.StorageURL,
	}
	if video.OwnerID != nil {
		ownerStr := video.OwnerID.String()
		event.OwnerID = &ownerStr
	}

	if err := s.publisher.PublishVideoUploaded(ctx, event); err != nil {
		logger.L().Error("failed to publish video.uploaded event", zap.Error(err), zap.String("video_id", video.ID.String()))
	}
}

func (s *videoService) validateMimeType(mimeType string) error {
	allowed := strings.Split(s.cfg.Upload.AllowedMimeTypes, ",")
	for _, a := range allowed {
		if strings.TrimSpace(a) == mimeType {
			return nil
		}
	}
	return errors.BadRequest(nil, fmt.Sprintf("mime type %q is not allowed", mimeType))
}

func mimeToExt(mimeType string) string {
	m := map[string]string{
		"video/mp4":       ".mp4",
		"video/quicktime": ".mov",
		"video/webm":      ".webm",
	}
	if ext, ok := m[mimeType]; ok {
		return ext
	}
	return filepath.Ext(mimeType)
}

func videoToResponse(v *domain.Video) *dto.VideoResponse {
	return &dto.VideoResponse{
		VideoID:         v.ID.String(),
		Title:           v.Title,
		Description:     v.Description,
		ObjectKey:       v.ObjectKey,
		Bucket:          v.Bucket,
		MimeType:        v.MimeType,
		SizeBytes:       v.SizeBytes,
		Status:          string(v.Status),
		StorageURL:      v.StorageURL,
		UploadExpiresAt: v.UploadExpiresAt,
		UploadedAt:      v.UploadedAt,
		CreatedAt:       v.CreatedAt,
	}
}

func videoToCompleteResponse(v *domain.Video) *dto.CompleteUploadResponse {
	return &dto.CompleteUploadResponse{
		VideoID:    v.ID.String(),
		Status:     string(v.Status),
		SizeBytes:  v.SizeBytes,
		Etag:       v.Etag,
		UploadedAt: v.UploadedAt,
	}
}
