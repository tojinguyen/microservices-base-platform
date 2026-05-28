package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backend/pkg/broker"
	"backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/tojinguyen/transcoder/internal/config"
	"github.com/tojinguyen/transcoder/internal/domain"
	"github.com/tojinguyen/transcoder/internal/dto"
	"github.com/tojinguyen/transcoder/internal/ffmpeg"
	"github.com/tojinguyen/transcoder/internal/playlist"
	"github.com/tojinguyen/transcoder/internal/publisher"
	"github.com/tojinguyen/transcoder/internal/repository"
	"github.com/tojinguyen/transcoder/internal/storage"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type JobProcessor struct {
	repo       repository.JobRepository
	storage    storage.ObjectStorage
	transcoder ffmpeg.Transcoder
	publisher  *publisher.EventPublisher
	cfg        *config.Config
}

func NewJobProcessor(
	repo repository.JobRepository,
	store storage.ObjectStorage,
	tr ffmpeg.Transcoder,
	pub *publisher.EventPublisher,
	cfg *config.Config,
) *JobProcessor {
	return &JobProcessor{
		repo:       repo,
		storage:    store,
		transcoder: tr,
		publisher:  pub,
		cfg:        cfg,
	}
}

// Process executes a single transcoding job. A returned non-nil error tells the
// caller to Nack (route to DLQ); nil means Ack the message.
//
// When the job permanently fails (retries exhausted) we still return nil so the
// message is acked — the dead state is recorded in DB and an event published.
func (p *JobProcessor) Process(ctx context.Context, body []byte) error {
	log := logger.L()

	var evt dto.VideoUploadedEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		log.Error("failed to unmarshal video.uploaded event", zap.Error(err))
		// Malformed message — send to DLQ.
		return broker.ErrRejectToDLQ
	}

	videoID, err := uuid.Parse(evt.VideoID)
	if err != nil {
		log.Error("invalid video_id in event", zap.String("video_id", evt.VideoID), zap.Error(err))
		return broker.ErrRejectToDLQ
	}

	sizePtr := evt.SizeBytes
	job := &domain.TranscodingJob{
		ID:         uuid.New(),
		VideoID:    videoID,
		Status:     domain.StatusPending,
		MaxRetries: p.cfg.Transcoder.MaxRetries,
		ObjectKey:  evt.ObjectKey,
		Bucket:     evt.Bucket,
		MimeType:   evt.MimeType,
		SizeBytes:  sizePtr,
	}

	existing, created, err := p.repo.FindOrCreateByVideoID(ctx, job)
	if err != nil {
		log.Error("failed to find/create job", zap.Error(err))
		return err
	}
	if !created {
		// Idempotency: skip if already completed; otherwise resume.
		switch existing.Status {
		case domain.StatusCompleted, domain.StatusDead:
			log.Info("job already in terminal state, skipping",
				zap.String("video_id", videoID.String()),
				zap.String("status", string(existing.Status)))
			return nil
		}
		job = existing
	}

	return p.runJob(ctx, job, evt)
}

func (p *JobProcessor) runJob(ctx context.Context, job *domain.TranscodingJob, evt dto.VideoUploadedEvent) error {
	log := logger.L()

	now := time.Now()
	job.Status = domain.StatusProcessing
	job.StartedAt = &now
	if err := p.repo.Update(ctx, job); err != nil {
		log.Error("failed to mark job processing", zap.Error(err))
		return err
	}

	tmpRoot := p.cfg.Transcoder.TmpDir
	if tmpRoot == "" {
		tmpRoot = os.TempDir()
	}
	jobTmp := filepath.Join(tmpRoot, job.ID.String())
	defer func() {
		if err := os.RemoveAll(jobTmp); err != nil {
			log.Warn("failed to cleanup tmp dir", zap.String("dir", jobTmp), zap.Error(err))
		}
	}()

	if err := os.MkdirAll(jobTmp, 0o755); err != nil {
		return p.handleFailure(ctx, job, fmt.Errorf("mkdir tmp: %w", err))
	}

	inputPath := filepath.Join(jobTmp, "input"+extensionFor(evt.MimeType, evt.ObjectKey))
	if err := p.downloadInput(ctx, evt.ObjectKey, inputPath); err != nil {
		return p.handleFailure(ctx, job, fmt.Errorf("download: %w", err))
	}

	outputDir := filepath.Join(jobTmp, "hls")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return p.handleFailure(ctx, job, fmt.Errorf("mkdir output: %w", err))
	}

	result, err := p.transcoder.Transcode(ctx, inputPath, outputDir, dto.DefaultRenditions)
	if err != nil {
		return p.handleFailure(ctx, job, fmt.Errorf("transcode: %w", err))
	}

	masterContent := playlist.GenerateMaster(result.Renditions)
	masterPath := filepath.Join(outputDir, "master.m3u8")
	if err := os.WriteFile(masterPath, []byte(masterContent), 0o644); err != nil {
		return p.handleFailure(ctx, job, fmt.Errorf("write master playlist: %w", err))
	}

	basePrefix := strings.TrimSuffix(p.cfg.Transcoder.HLSPrefix, "/")
	if basePrefix == "" {
		basePrefix = "hls"
	}
	hlsBasePath := fmt.Sprintf("%s/%s", basePrefix, job.VideoID.String())

	if err := p.uploadHLSTree(ctx, outputDir, hlsBasePath); err != nil {
		return p.handleFailure(ctx, job, fmt.Errorf("upload hls: %w", err))
	}

	renditionsOut := make([]dto.RenditionOutput, 0, len(result.Renditions))
	for _, r := range result.Renditions {
		renditionsOut = append(renditionsOut, dto.RenditionOutput{
			Name:        r.Name,
			Playlist:    fmt.Sprintf("%s/%s/%s.m3u8", hlsBasePath, r.Name, r.Name),
			BitrateKbps: r.BitrateKbps,
			Width:       r.Width,
			Height:      r.Height,
		})
	}
	renditionsJSON, _ := json.Marshal(renditionsOut)

	completedAt := time.Now()
	durationCopy := result.DurationSeconds
	outputSizeCopy := result.OutputSizeBytes
	job.Status = domain.StatusCompleted
	job.HLSBasePath = hlsBasePath + "/"
	job.MasterPlaylist = fmt.Sprintf("%s/master.m3u8", hlsBasePath)
	job.Renditions = renditionsJSON
	job.DurationSeconds = &durationCopy
	job.OutputSizeBytes = &outputSizeCopy
	job.CompletedAt = &completedAt
	job.ErrorMessage = ""
	if err := p.repo.Update(ctx, job); err != nil {
		log.Error("failed to update job to completed", zap.Error(err))
		return err
	}

	transcodedEvt := dto.VideoTranscodedEvent{
		VideoID:         job.VideoID.String(),
		JobID:           job.ID.String(),
		HLSBasePath:     job.HLSBasePath,
		MasterPlaylist:  job.MasterPlaylist,
		Renditions:      renditionsOut,
		DurationSeconds: result.DurationSeconds,
		OutputSizeBytes: result.OutputSizeBytes,
		TranscodedAt:    completedAt,
	}
	if err := p.publisher.PublishVideoTranscoded(ctx, transcodedEvt); err != nil {
		log.Error("failed to publish video.transcoded", zap.Error(err))
		// Don't fail the job — DB is updated; reconciler can re-publish later.
	}

	log.Info("transcoding job completed",
		zap.String("job_id", job.ID.String()),
		zap.String("video_id", job.VideoID.String()),
		zap.Float64("duration_seconds", result.DurationSeconds),
		zap.Int64("output_size_bytes", result.OutputSizeBytes),
	)
	return nil
}

func (p *JobProcessor) handleFailure(ctx context.Context, job *domain.TranscodingJob, jobErr error) error {
	log := logger.L()
	log.Error("transcoding job failed",
		zap.String("job_id", job.ID.String()),
		zap.String("video_id", job.VideoID.String()),
		zap.Int("retry_count", job.RetryCount),
		zap.Error(jobErr),
	)

	job.RetryCount++
	job.ErrorMessage = jobErr.Error()

	if job.RetryCount >= job.MaxRetries {
		job.Status = domain.StatusDead
		if err := p.repo.Update(ctx, job); err != nil {
			log.Error("failed to update job to dead", zap.Error(err))
			return err
		}
		failedEvt := dto.VideoTranscodeFailedEvent{
			VideoID:      job.VideoID.String(),
			JobID:        job.ID.String(),
			ErrorMessage: job.ErrorMessage,
			RetryCount:   job.RetryCount,
			FailedAt:     time.Now(),
		}
		if err := p.publisher.PublishVideoTranscodeFailed(ctx, failedEvt); err != nil {
			log.Error("failed to publish video.transcode_failed", zap.Error(err))
		}
		// Ack — no point requeueing.
		return nil
	}

	job.Status = domain.StatusFailed
	if err := p.repo.Update(ctx, job); err != nil {
		log.Error("failed to update job to failed", zap.Error(err))
		return err
	}
	// Tell broker to Nack & route to DLQ so it doesn't loop the main queue.
	return broker.ErrRejectToDLQ
}

func (p *JobProcessor) downloadInput(ctx context.Context, key, dst string) error {
	reader, err := p.storage.GetObject(ctx, key)
	if err != nil {
		return err
	}
	defer reader.Close()

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, reader); err != nil {
		return err
	}
	return nil
}

func (p *JobProcessor) uploadHLSTree(ctx context.Context, localDir, remotePrefix string) error {
	concurrency := p.cfg.Transcoder.UploadConcurrency
	if concurrency <= 0 {
		concurrency = 10
	}

	type fileItem struct {
		localPath  string
		remoteKey  string
		contentTyp string
		size       int64
	}

	var items []fileItem
	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(localDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		remoteKey := remotePrefix + "/" + rel
		items = append(items, fileItem{
			localPath:  path,
			remoteKey:  remoteKey,
			contentTyp: contentTypeFor(rel),
			size:       info.Size(),
		})
		return nil
	})
	if err != nil {
		return err
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	for _, item := range items {
		it := item
		g.Go(func() error {
			f, err := os.Open(it.localPath)
			if err != nil {
				return err
			}
			defer f.Close()
			return p.storage.PutObject(gctx, it.remoteKey, it.contentTyp, f, it.size)
		})
	}
	return g.Wait()
}

func extensionFor(mimeType, key string) string {
	if ext := filepath.Ext(key); ext != "" {
		return ext
	}
	if exts, _ := mime.ExtensionsByType(mimeType); len(exts) > 0 {
		return exts[0]
	}
	return ".mp4"
}

func contentTypeFor(path string) string {
	switch {
	case strings.HasSuffix(path, ".m3u8"):
		return "application/vnd.apple.mpegurl"
	case strings.HasSuffix(path, ".ts"):
		return "video/mp2t"
	default:
		if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
			return ct
		}
		return "application/octet-stream"
	}
}

