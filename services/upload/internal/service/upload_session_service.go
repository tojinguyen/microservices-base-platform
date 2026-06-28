package service

import (
	"context"
	"fmt"
	"math"
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

const minPartSizeBytes = 5 * 1024 * 1024   // S3 minimum 5 MB per part
const maxPartCount = 10000                  // S3 maximum parts per upload

type UploadSessionService interface {
	InitChunkedUpload(ctx context.Context, req dto.InitChunkedUploadRequest) (*dto.InitChunkedUploadResponse, error)
	GetSession(ctx context.Context, sessionID uuid.UUID) (*dto.GetSessionResponse, error)
	GetPartURL(ctx context.Context, sessionID uuid.UUID, partNumber int) (*dto.GetPartURLResponse, error)
	ConfirmPart(ctx context.Context, sessionID uuid.UUID, partNumber int, req dto.ConfirmPartRequest) (*dto.ConfirmPartResponse, error)
	CompleteSession(ctx context.Context, sessionID uuid.UUID) (*dto.CompleteChunkedUploadResponse, error)
	AbortSession(ctx context.Context, sessionID uuid.UUID) (*dto.AbortSessionResponse, error)
}

type uploadSessionService struct {
	sessionRepo repository.UploadSessionRepository
	videoRepo   repository.VideoRepository
	storage     storage.ObjectStorage
	publisher   *publisher.EventPublisher
	cfg         *config.Config
}

func NewUploadSessionService(
	sessionRepo repository.UploadSessionRepository,
	videoRepo repository.VideoRepository,
	store storage.ObjectStorage,
	pub *publisher.EventPublisher,
	cfg *config.Config,
) UploadSessionService {
	return &uploadSessionService{
		sessionRepo: sessionRepo,
		videoRepo:   videoRepo,
		storage:     store,
		publisher:   pub,
		cfg:         cfg,
	}
}

func (s *uploadSessionService) InitChunkedUpload(ctx context.Context, req dto.InitChunkedUploadRequest) (*dto.InitChunkedUploadResponse, error) {
	if err := s.validateMimeType(req.MimeType); err != nil {
		return nil, err
	}
	if req.SizeBytes > s.cfg.Upload.MaxSizeBytes {
		return nil, errors.BadRequest(nil, fmt.Sprintf("file size exceeds limit of %d bytes", s.cfg.Upload.MaxSizeBytes))
	}

	chunkSizeMB := s.cfg.Upload.ChunkSizeMB
	if chunkSizeMB < 5 {
		chunkSizeMB = 5
	}
	partSize := int64(chunkSizeMB) * 1024 * 1024
	if partSize < minPartSizeBytes {
		partSize = minPartSizeBytes
	}

	totalParts := int(math.Ceil(float64(req.SizeBytes) / float64(partSize)))
	if totalParts > maxPartCount {
		return nil, errors.BadRequest(nil, fmt.Sprintf("file requires %d parts which exceeds the maximum of %d", totalParts, maxPartCount))
	}
	if totalParts < 1 {
		totalParts = 1
	}

	videoID, err := uuid.Parse(req.VideoID)
	if err != nil {
		return nil, errors.BadRequest(nil, "invalid video_id")
	}

	video, err := s.videoRepo.GetByID(ctx, videoID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.NotFound("video not found")
		}
		return nil, errors.InternalServer(err)
	}
	if video.Status != domain.StatusPendingUpload {
		return nil, errors.BadRequest(nil, fmt.Sprintf("video status is %q, expected %q", video.Status, domain.StatusPendingUpload))
	}

	s3UploadID, err := s.storage.CreateMultipartUpload(ctx, video.ObjectKey, req.MimeType)
	if err != nil {
		logger.L().Error("failed to create multipart upload", zap.Error(err))
		return nil, errors.InternalServer(err)
	}

	sessionID := uuid.New()
	sessionTTL := time.Duration(s.cfg.Upload.SessionTTLHours) * time.Hour
	if sessionTTL == 0 {
		sessionTTL = 24 * time.Hour
	}

	session := &domain.UploadSession{
		ID:             sessionID,
		VideoID:        videoID,
		S3UploadID:     s3UploadID,
		ObjectKey:      video.ObjectKey,
		Bucket:         video.Bucket,
		MimeType:       req.MimeType,
		TotalParts:     totalParts,
		PartSizeBytes:  partSize,
		TotalSizeBytes: req.SizeBytes,
		Status:         domain.SessionStatusInitiated,
		ExpiresAt:      time.Now().Add(sessionTTL),
	}

	parts := make([]domain.UploadPart, totalParts)
	for i := 0; i < totalParts; i++ {
		parts[i] = domain.UploadPart{
			ID:         uuid.New(),
			SessionID:  sessionID,
			PartNumber: i + 1,
			Status:     domain.PartStatusPending,
		}
	}

	if err := s.sessionRepo.CreateSession(ctx, session, parts); err != nil {
		logger.L().Error("failed to create upload session", zap.Error(err))
		_ = s.storage.AbortMultipartUpload(ctx, video.ObjectKey, s3UploadID)
		return nil, errors.InternalServer(err)
	}

	return &dto.InitChunkedUploadResponse{
		SessionID:  sessionID.String(),
		VideoID:    videoID.String(),
		TotalParts: totalParts,
		PartSize:   partSize,
		TotalSize:  req.SizeBytes,
		Status:     string(domain.SessionStatusInitiated),
		ExpiresAt:  session.ExpiresAt,
	}, nil
}

func (s *uploadSessionService) GetSession(ctx context.Context, sessionID uuid.UUID) (*dto.GetSessionResponse, error) {
	session, err := s.sessionRepo.GetSessionWithParts(ctx, sessionID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.NotFound("upload session not found")
		}
		return nil, errors.InternalServer(err)
	}

	parts := make([]dto.PartStateResponse, len(session.Parts))
	for i, p := range session.Parts {
		parts[i] = dto.PartStateResponse{
			PartNumber: p.PartNumber,
			Status:     string(p.Status),
			Etag:       p.Etag,
		}
	}

	return &dto.GetSessionResponse{
		SessionID:      session.ID.String(),
		VideoID:        session.VideoID.String(),
		TotalParts:     session.TotalParts,
		PartSizeBytes:  session.PartSizeBytes,
		TotalSizeBytes: session.TotalSizeBytes,
		Status:         string(session.Status),
		ExpiresAt:      session.ExpiresAt,
		Parts:          parts,
	}, nil
}

func (s *uploadSessionService) GetPartURL(ctx context.Context, sessionID uuid.UUID, partNumber int) (*dto.GetPartURLResponse, error) {
	session, err := s.sessionRepo.GetSessionByID(ctx, sessionID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.NotFound("upload session not found")
		}
		return nil, errors.InternalServer(err)
	}

	if session.Status != domain.SessionStatusInitiated && session.Status != domain.SessionStatusInProgress {
		return nil, errors.BadRequest(nil, fmt.Sprintf("session status is %q, cannot request part URL", session.Status))
	}
	if partNumber < 1 || partNumber > session.TotalParts {
		return nil, errors.BadRequest(nil, fmt.Sprintf("part number must be between 1 and %d", session.TotalParts))
	}

	part, err := s.sessionRepo.GetPartByNumber(ctx, sessionID, partNumber)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.NotFound("part not found")
		}
		return nil, errors.InternalServer(err)
	}
	if part.Status == domain.PartStatusUploaded {
		return nil, errors.BadRequest(nil, fmt.Sprintf("part %d is already uploaded", partNumber))
	}

	ttl := time.Duration(s.cfg.Upload.PartPresignTTLSeconds) * time.Second
	if ttl == 0 {
		ttl = time.Hour
	}

	url, expiresAt, err := s.storage.GeneratePresignedPartURL(ctx, session.ObjectKey, session.S3UploadID, int32(partNumber), ttl)
	if err != nil {
		logger.L().Error("failed to generate presigned part URL", zap.Error(err))
		return nil, errors.InternalServer(err)
	}

	part.PresignURL = url
	part.PresignExpiresAt = &expiresAt
	if err := s.sessionRepo.UpdatePart(ctx, part); err != nil {
		logger.L().Warn("failed to save presign URL to part", zap.Error(err))
	}

	if session.Status == domain.SessionStatusInitiated {
		if _, err := s.sessionRepo.TransitionSessionStatus(ctx, sessionID, domain.SessionStatusInitiated, domain.SessionStatusInProgress); err != nil {
			logger.L().Warn("failed to transition session to in_progress", zap.Error(err))
		}
	}

	return &dto.GetPartURLResponse{
		SessionID:  sessionID.String(),
		PartNumber: partNumber,
		UploadURL:  url,
		Method:     "PUT",
		ExpiresAt:  expiresAt,
	}, nil
}

func (s *uploadSessionService) ConfirmPart(ctx context.Context, sessionID uuid.UUID, partNumber int, req dto.ConfirmPartRequest) (*dto.ConfirmPartResponse, error) {
	session, err := s.sessionRepo.GetSessionByID(ctx, sessionID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.NotFound("upload session not found")
		}
		return nil, errors.InternalServer(err)
	}

	if session.Status != domain.SessionStatusInitiated && session.Status != domain.SessionStatusInProgress {
		return nil, errors.BadRequest(nil, fmt.Sprintf("session status is %q, cannot confirm part", session.Status))
	}

	part, err := s.sessionRepo.GetPartByNumber(ctx, sessionID, partNumber)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.NotFound("part not found")
		}
		return nil, errors.InternalServer(err)
	}
	if part.Status == domain.PartStatusUploaded {
		return nil, errors.BadRequest(nil, fmt.Sprintf("part %d is already confirmed", partNumber))
	}

	etag := strings.Trim(req.Etag, `"`)
	if etag == "" {
		return nil, errors.BadRequest(nil, "etag must not be empty")
	}

	now := time.Now()
	part.Etag = etag
	part.Status = domain.PartStatusUploaded
	part.UploadedAt = &now

	if err := s.sessionRepo.UpdatePart(ctx, part); err != nil {
		return nil, errors.InternalServer(err)
	}

	uploadedParts, err := s.sessionRepo.GetUploadedParts(ctx, sessionID)
	if err != nil {
		return nil, errors.InternalServer(err)
	}

	return &dto.ConfirmPartResponse{
		SessionID:     sessionID.String(),
		PartNumber:    partNumber,
		Status:        string(domain.PartStatusUploaded),
		UploadedParts: len(uploadedParts),
		TotalParts:    session.TotalParts,
	}, nil
}

func (s *uploadSessionService) CompleteSession(ctx context.Context, sessionID uuid.UUID) (*dto.CompleteChunkedUploadResponse, error) {
	session, err := s.sessionRepo.GetSessionWithParts(ctx, sessionID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.NotFound("upload session not found")
		}
		return nil, errors.InternalServer(err)
	}

	if session.Status == domain.SessionStatusCompleted {
		video, _ := s.videoRepo.GetByID(ctx, session.VideoID)
		return sessionToCompleteResponse(session, video), nil
	}

	if session.Status != domain.SessionStatusInProgress {
		return nil, errors.BadRequest(nil, fmt.Sprintf("session status is %q, cannot complete", session.Status))
	}

	var pending []int
	for _, p := range session.Parts {
		if p.Status != domain.PartStatusUploaded {
			pending = append(pending, p.PartNumber)
		}
	}
	if len(pending) > 0 {
		return nil, errors.BadRequest(nil, fmt.Sprintf("parts not yet uploaded: %v", pending))
	}

	// Optimistic lock: only one caller can transition to "completing"
	ok, err := s.sessionRepo.TransitionSessionStatus(ctx, sessionID, domain.SessionStatusInProgress, domain.SessionStatusCompleting)
	if err != nil {
		return nil, errors.InternalServer(err)
	}
	if !ok {
		return nil, errors.BadRequest(nil, "session is already being completed by another request")
	}

	completedParts := make([]storage.CompletedPart, len(session.Parts))
	for i, p := range session.Parts {
		completedParts[i] = storage.CompletedPart{
			PartNumber: int32(p.PartNumber),
			ETag:       p.Etag,
		}
	}

	etag, err := s.storage.CompleteMultipartUpload(ctx, session.ObjectKey, session.S3UploadID, completedParts)
	if err != nil {
		logger.L().Error("failed to complete multipart upload on S3", zap.Error(err))
		_, _ = s.sessionRepo.TransitionSessionStatus(ctx, sessionID, domain.SessionStatusCompleting, domain.SessionStatusInProgress)
		return nil, errors.InternalServer(err)
	}

	now := time.Now()

	video, err := s.videoRepo.GetByID(ctx, session.VideoID)
	if err != nil {
		return nil, errors.InternalServer(err)
	}
	video.Status = domain.StatusUploaded
	video.Etag = etag
	video.SizeBytes = &session.TotalSizeBytes
	video.UploadedAt = &now
	if err := s.videoRepo.Update(ctx, video); err != nil {
		logger.L().Error("failed to update video after completing multipart upload", zap.Error(err))
		return nil, errors.InternalServer(err)
	}

	session.Status = domain.SessionStatusCompleted
	session.CompletedAt = &now
	if err := s.sessionRepo.UpdateSession(ctx, session); err != nil {
		logger.L().Warn("failed to mark session as completed", zap.Error(err))
	}

	s.publishUploadedEvent(ctx, video)

	return sessionToCompleteResponse(session, video), nil
}

func (s *uploadSessionService) AbortSession(ctx context.Context, sessionID uuid.UUID) (*dto.AbortSessionResponse, error) {
	session, err := s.sessionRepo.GetSessionByID(ctx, sessionID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.NotFound("upload session not found")
		}
		return nil, errors.InternalServer(err)
	}

	if session.Status == domain.SessionStatusAborted {
		return &dto.AbortSessionResponse{SessionID: sessionID.String(), Status: string(session.Status)}, nil
	}

	if err := s.storage.AbortMultipartUpload(ctx, session.ObjectKey, session.S3UploadID); err != nil {
		logger.L().Warn("failed to abort S3 multipart upload", zap.Error(err), zap.String("session_id", sessionID.String()))
	}

	session.Status = domain.SessionStatusAborted
	if err := s.sessionRepo.UpdateSession(ctx, session); err != nil {
		return nil, errors.InternalServer(err)
	}

	return &dto.AbortSessionResponse{SessionID: sessionID.String(), Status: string(domain.SessionStatusAborted)}, nil
}

func (s *uploadSessionService) publishUploadedEvent(ctx context.Context, video *domain.Video) {
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

func (s *uploadSessionService) validateMimeType(mimeType string) error {
	allowed := strings.Split(s.cfg.Upload.AllowedMimeTypes, ",")
	for _, a := range allowed {
		if strings.TrimSpace(a) == mimeType {
			return nil
		}
	}
	return errors.BadRequest(nil, fmt.Sprintf("mime type %q is not allowed", mimeType))
}

func sessionToCompleteResponse(session *domain.UploadSession, video *domain.Video) *dto.CompleteChunkedUploadResponse {
	resp := &dto.CompleteChunkedUploadResponse{
		SessionID: session.ID.String(),
		VideoID:   session.VideoID.String(),
		Status:    string(session.Status),
	}
	if video != nil {
		resp.Etag = video.Etag
		resp.SizeBytes = video.SizeBytes
		resp.UploadedAt = video.UploadedAt
	}
	return resp
}
