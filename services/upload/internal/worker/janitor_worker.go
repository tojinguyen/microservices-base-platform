package worker

import (
	"context"
	"time"

	"backend/pkg/logger"

	"github.com/tojinguyen/upload/internal/config"
	"github.com/tojinguyen/upload/internal/domain"
	"github.com/tojinguyen/upload/internal/dto"
	"github.com/tojinguyen/upload/internal/publisher"
	"github.com/tojinguyen/upload/internal/repository"
	"github.com/tojinguyen/upload/internal/storage"
	"go.uber.org/zap"
)

type JanitorWorker struct {
	repo        repository.VideoRepository
	sessionRepo repository.UploadSessionRepository
	storage     storage.ObjectStorage
	publisher   *publisher.EventPublisher
	cfg         *config.Config
}

func NewJanitorWorker(
	repo repository.VideoRepository,
	sessionRepo repository.UploadSessionRepository,
	store storage.ObjectStorage,
	pub *publisher.EventPublisher,
	cfg *config.Config,
) *JanitorWorker {
	return &JanitorWorker{
		repo:        repo,
		sessionRepo: sessionRepo,
		storage:     store,
		publisher:   pub,
		cfg:         cfg,
	}
}

func (w *JanitorWorker) Start(ctx context.Context) {
	log := logger.L()
	interval := time.Duration(w.cfg.Janitor.IntervalSeconds) * time.Second
	log.Info("janitor worker started", zap.Duration("interval", interval))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("janitor worker stopping")
			return
		case <-ticker.C:
			w.runBatch(ctx)
			w.runSessionBatch(ctx)
		}
	}
}

func (w *JanitorWorker) runBatch(ctx context.Context) {
	log := logger.L()

	videos, err := w.repo.ListExpiredPending(ctx, time.Now(), w.cfg.Janitor.BatchSize)
	if err != nil {
		log.Error("janitor: failed to query expired pending videos", zap.Error(err))
		return
	}

	if len(videos) == 0 {
		return
	}

	log.Info("janitor: processing expired pending videos", zap.Int("count", len(videos)))

	for i := range videos {
		w.processVideo(ctx, &videos[i])
	}
}

func (w *JanitorWorker) runSessionBatch(ctx context.Context) {
	log := logger.L()

	sessions, err := w.sessionRepo.ListExpiredActiveSessions(ctx, time.Now(), w.cfg.Janitor.BatchSize)
	if err != nil {
		log.Error("janitor: failed to query expired upload sessions", zap.Error(err))
		return
	}

	if len(sessions) == 0 {
		return
	}

	log.Info("janitor: expiring stale upload sessions", zap.Int("count", len(sessions)))

	for i := range sessions {
		s := &sessions[i]
		if err := w.storage.AbortMultipartUpload(ctx, s.ObjectKey, s.S3UploadID); err != nil {
			log.Warn("janitor: failed to abort S3 multipart upload",
				zap.Error(err),
				zap.String("session_id", s.ID.String()),
			)
		}
		s.Status = domain.SessionStatusExpired
		if err := w.sessionRepo.UpdateSession(ctx, s); err != nil {
			log.Error("janitor: failed to mark session as expired",
				zap.Error(err),
				zap.String("session_id", s.ID.String()),
			)
		} else {
			log.Info("janitor: marked upload session as expired", zap.String("session_id", s.ID.String()))
		}
	}
}

func (w *JanitorWorker) processVideo(ctx context.Context, video *domain.Video) {
	log := logger.L()

	sizeBytes, etag, exists, err := w.storage.HeadObject(ctx, video.ObjectKey)
	if err != nil {
		log.Error("janitor: head object failed", zap.Error(err), zap.String("video_id", video.ID.String()))
		return
	}

	if exists {
		now := time.Now()
		video.Status = domain.StatusUploaded
		video.SizeBytes = &sizeBytes
		video.Etag = etag
		video.UploadedAt = &now

		if err := w.repo.Update(ctx, video); err != nil {
			log.Error("janitor: failed to update video to uploaded", zap.Error(err), zap.String("video_id", video.ID.String()))
			return
		}

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

		if err := w.publisher.PublishVideoUploaded(ctx, event); err != nil {
			log.Error("janitor: failed to publish video.uploaded event", zap.Error(err), zap.String("video_id", video.ID.String()))
		} else {
			log.Info("janitor: recovered uploaded video", zap.String("video_id", video.ID.String()))
		}
	} else {
		video.Status = domain.StatusExpired
		if err := w.repo.Update(ctx, video); err != nil {
			log.Error("janitor: failed to mark video as expired", zap.Error(err), zap.String("video_id", video.ID.String()))
			return
		}
		log.Info("janitor: marked video as expired", zap.String("video_id", video.ID.String()))
	}
}
