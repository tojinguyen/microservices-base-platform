package main

import (
	"backend/pkg/broker"
	"backend/pkg/config"
	"backend/pkg/db"
	"backend/pkg/logger"
	"backend/pkg/ratelimit"
	"backend/pkg/redis"
	"backend/pkg/trace"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/tojinguyen/upload/docs"
	uploadConfig "github.com/tojinguyen/upload/internal/config"
	"github.com/tojinguyen/upload/internal/handler"
	"github.com/tojinguyen/upload/internal/publisher"
	"github.com/tojinguyen/upload/internal/repository"
	"github.com/tojinguyen/upload/internal/route"
	"github.com/tojinguyen/upload/internal/service"
	"github.com/tojinguyen/upload/internal/storage"
	"github.com/tojinguyen/upload/internal/worker"
	"github.com/tojinguyen/upload/migrations"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// @title Upload Service API
// @version 1.0
// @description Video upload service — presigned URL upload, metadata management, and event publishing.
// @host localhost
// @BasePath /api/v1

func main() {
	if err := logger.Init("upload-service", "dev"); err != nil {
		panic(err)
	}
	log := logger.L()

	cfg := &uploadConfig.Config{}
	if err := config.Load(cfg); err != nil {
		log.Panic("failed to load upload configuration", zap.Error(err))
	}

	database, err := db.New(cfg.Database)
	if err != nil {
		log.Panic("failed to connect to database", zap.Error(err))
	}

	sqlDB, err := database.DB()
	if err != nil {
		log.Panic("failed to get sql.DB from gorm", zap.Error(err))
	}

	if err := db.RunMigrations(sqlDB, migrations.FS, "."); err != nil {
		log.Panic("failed to run database migrations", zap.Error(err))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.Otel.Enabled && cfg.Otel.ExporterEndpoint != "" {
		tp, err := trace.InitTracer(ctx, "upload-service", cfg.Otel.ExporterEndpoint)
		if err != nil {
			log.Warn("failed to initialize OpenTelemetry tracer, tracing disabled", zap.Error(err))
		} else {
			defer func() {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := tp.Shutdown(shutdownCtx); err != nil {
					log.Error("failed to shutdown tracer provider", zap.Error(err))
				}
			}()
			log.Info("OpenTelemetry tracing initialized", zap.String("endpoint", cfg.Otel.ExporterEndpoint))
		}
	}

	log.Info("Starting upload service", zap.String("mode", cfg.AppMode))

	switch cfg.AppMode {
	case uploadConfig.ModeWorkerJanitor:
		runJanitorWorker(ctx, cfg, database)
	case uploadConfig.ModeAPI:
		runAPI(ctx, cfg, database)
	default:
		log.Error("unknown app mode, falling back to API mode", zap.String("mode", cfg.AppMode))
		runAPI(ctx, cfg, database)
	}
}

func runAPI(ctx context.Context, cfg *uploadConfig.Config, database *gorm.DB) {
	log := logger.L()

	brokerClient, err := broker.NewRabbitMQ(cfg.Broker)
	if err != nil {
		log.Panic("failed to connect to broker", zap.Error(err))
	}
	defer brokerClient.Close()

	redisClient, err := redis.New(cfg.Redis)
	if err != nil {
		log.Panic("failed to connect to redis", zap.Error(err))
	}

	store, err := storage.NewS3Storage(cfg.Storage)
	if err != nil {
		log.Panic("failed to initialize storage client", zap.Error(err))
	}

	limiter := ratelimit.New(redisClient, ratelimit.Config{
		Limit:  cfg.RateLimit.Limit,
		Window: time.Duration(cfg.RateLimit.WindowSecs) * time.Second,
	})

	videoRepo := repository.NewVideoRepository(database)
	eventPub := publisher.NewEventPublisher(brokerClient)
	videoSvc := service.NewVideoService(videoRepo, store, eventPub, cfg)
	videoHandler := handler.NewVideoHandler(videoSvc)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(logger.GinMiddleware())
	r.Use(trace.TracerMiddleware("upload-service"))
	route.RegisterRoutes(r, videoHandler, limiter, cfg.RateLimit)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.ServerPort),
		Handler: r,
	}

	go func() {
		log.Info("server starting", zap.Int("port", cfg.ServerPort))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("failed to run server", zap.Error(err))
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeGrace)*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatal("server forced to shutdown", zap.Error(err))
	}
	log.Info("API server gracefully stopped")
}

func runJanitorWorker(ctx context.Context, cfg *uploadConfig.Config, database *gorm.DB) {
	log := logger.L()

	brokerClient, err := broker.NewRabbitMQ(cfg.Broker)
	if err != nil {
		log.Panic("failed to connect to broker", zap.Error(err))
	}
	defer brokerClient.Close()

	store, err := storage.NewS3Storage(cfg.Storage)
	if err != nil {
		log.Panic("failed to initialize storage client", zap.Error(err))
	}

	videoRepo := repository.NewVideoRepository(database)
	eventPub := publisher.NewEventPublisher(brokerClient)
	janitor := worker.NewJanitorWorker(videoRepo, store, eventPub, cfg)

	log.Info("Janitor worker starting")
	janitor.Start(ctx)
}
