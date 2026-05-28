package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"backend/pkg/broker"
	"backend/pkg/config"
	"backend/pkg/db"
	"backend/pkg/logger"
	"backend/pkg/trace"

	transcoderConfig "github.com/tojinguyen/transcoder/internal/config"
	"github.com/tojinguyen/transcoder/internal/ffmpeg"
	"github.com/tojinguyen/transcoder/internal/publisher"
	"github.com/tojinguyen/transcoder/internal/repository"
	"github.com/tojinguyen/transcoder/internal/storage"
	"github.com/tojinguyen/transcoder/internal/worker"
	"github.com/tojinguyen/transcoder/migrations"

	"go.uber.org/zap"
)

func main() {
	if err := logger.Init("transcoder-service", "dev"); err != nil {
		panic(err)
	}
	log := logger.L()

	cfg := &transcoderConfig.Config{}
	if err := config.Load(cfg); err != nil {
		log.Panic("failed to load transcoder configuration", zap.Error(err))
	}
	if cfg.AppMode == "" {
		cfg.AppMode = transcoderConfig.ModeWorker
	}

	database, err := db.New(cfg.Database)
	if err != nil {
		log.Panic("failed to connect to database", zap.Error(err))
	}
	sqlDB, err := database.DB()
	if err != nil {
		log.Panic("failed to get sql.DB", zap.Error(err))
	}
	if err := db.RunMigrations(sqlDB, migrations.FS, "."); err != nil {
		log.Panic("failed to run database migrations", zap.Error(err))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.Otel.Enabled && cfg.Otel.ExporterEndpoint != "" {
		tp, err := trace.InitTracer(ctx, "transcoder-service", cfg.Otel.ExporterEndpoint)
		if err != nil {
			log.Warn("failed to init OTel tracer", zap.Error(err))
		} else {
			defer func() {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = tp.Shutdown(shutdownCtx)
			}()
		}
	}

	brokerClient, err := broker.NewRabbitMQ(cfg.Broker)
	if err != nil {
		log.Panic("failed to connect to broker", zap.Error(err))
	}
	defer brokerClient.Close()

	store, err := storage.NewS3Storage(cfg.Storage)
	if err != nil {
		log.Panic("failed to init storage", zap.Error(err))
	}

	jobRepo := repository.NewJobRepository(database)
	eventPub := publisher.NewEventPublisher(brokerClient)
	transcoder := ffmpeg.NewFFmpegTranscoder(cfg.Transcoder.FFmpegBinary, cfg.Transcoder.SegmentDuration)
	processor := worker.NewJobProcessor(jobRepo, store, transcoder, eventPub, cfg)
	w := worker.NewTranscoderWorker(brokerClient, processor, cfg)

	log.Info("transcoder service starting", zap.String("mode", cfg.AppMode))
	if err := w.Start(ctx); err != nil {
		log.Panic("failed to start worker", zap.Error(err))
	}

	<-ctx.Done()
	log.Info("shutdown signal received, draining...",
		zap.Int("grace_seconds", cfg.TimeGrace))

	graceDur := time.Duration(cfg.TimeGrace) * time.Second
	if graceDur <= 0 {
		graceDur = 15 * time.Second
	}
	time.Sleep(graceDur)
	log.Info("transcoder service stopped")
}
