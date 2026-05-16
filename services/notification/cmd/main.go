package main

import (
	"backend/pkg/broker"
	"backend/pkg/config"
	"backend/pkg/db"
	"backend/pkg/logger"
	"backend/pkg/ratelimit"
	"backend/pkg/redis"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/tojinguyen/notification/docs"
	notificationConfig "github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/handler"
	"github.com/tojinguyen/notification/internal/repository"
	"github.com/tojinguyen/notification/internal/route"
	"github.com/tojinguyen/notification/internal/service"
	"github.com/tojinguyen/notification/internal/worker"
	"github.com/tojinguyen/notification/migrations"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// @title Notification Service API
// @version 1.0
// @description This is a notification service API
// @host localhost
// @BasePath /api/v1/notifications

func main() {
	if err := logger.Init("notification-service", "dev"); err != nil {
		panic(err)
	}
	log := logger.L()

	cfg := &notificationConfig.Config{}
	if err := config.Load(cfg); err != nil {
		log.Panic("failed to load notification configuration", zap.Error(err))
	}

	database, err := db.New(cfg.Database)
	if err != nil {
		log.Panic("failed to connect to database", zap.Error(err))
	}

	sqlDB, err := database.DB()
	if err != nil {
		log.Panic("failed to get sql.DB from gorm", zap.Error(err))
	}

	// Always run migrations
	if err := db.RunMigrations(sqlDB, migrations.FS, "."); err != nil {
		log.Panic("failed to run database migrations", zap.Error(err))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("Starting notification service", zap.String("mode", cfg.AppMode))

	switch cfg.AppMode {
	case notificationConfig.ModeWorkerPending:
		runPendingWorker(ctx, cfg, database)
	case notificationConfig.ModeWorkerEmail:
		runEmailWorker(ctx, cfg, database)
	case notificationConfig.ModeWorkerWebhook:
		runWebhookWorker(ctx, cfg, database)
	case notificationConfig.ModeWorkerScheduler:
		runSchedulerWorker(ctx, cfg, database)
	case notificationConfig.ModeWorkerCampaign:
		runCampaignWorker(ctx, cfg, database)
	case notificationConfig.ModeAPI:
		runAPI(ctx, cfg, database)
	default:
		log.Warn("Unknown app mode, falling back to API mode", zap.String("mode", cfg.AppMode))
		runAPI(ctx, cfg, database)
	}
}

func runAPI(ctx context.Context, cfg *notificationConfig.Config, database *gorm.DB) {
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

	cache := redis.NewCache(redisClient)

	limiter := ratelimit.New(redisClient, ratelimit.Config{
		Limit:  cfg.RateLimit.GlobalLimit,
		Window: time.Duration(cfg.RateLimit.GlobalWindowSecs) * time.Second,
	})

	notificationRepo := repository.NewNotificationRepository(database)
	templateRepo := repository.NewTemplateRepository(database)
	prefRepo := repository.NewPreferenceRepository(database)
	prefSvc := service.NewPreferenceService(prefRepo, cache)
	notificationService := service.NewNotificationService(notificationRepo, templateRepo, prefSvc, brokerClient, cfg)

	if err := notificationService.SeedTemplates(context.Background()); err != nil {
		log.Error("failed to seed notification templates", zap.Error(err))
	}

	scheduleRepo := repository.NewScheduleRepository(database)
	schedulerSvc := service.NewSchedulerService(scheduleRepo, templateRepo, notificationService)

	campaignRepo := repository.NewCampaignRepository(database)
	campaignSvc := service.NewCampaignService(campaignRepo)

	notificationHandler := handler.NewNotificationHandler(notificationService)
	preferenceHandler := handler.NewPreferenceHandler(prefSvc)
	scheduleHandler := handler.NewScheduleHandler(schedulerSvc)
	campaignHandler := handler.NewCampaignHandler(campaignSvc)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(logger.GinMiddleware())
	route.RegisterRoutes(r, notificationHandler, preferenceHandler, scheduleHandler, campaignHandler, limiter, cfg.RateLimit)

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

func runPendingWorker(ctx context.Context, cfg *notificationConfig.Config, database *gorm.DB) {
	log := logger.L()
	brokerClient, err := broker.NewRabbitMQ(cfg.Broker)
	if err != nil {
		log.Panic("failed to connect to broker", zap.Error(err))
	}
	defer brokerClient.Close()

	notificationRepo := repository.NewNotificationRepository(database)
	pendingWorker := worker.NewNotificationWorker(notificationRepo, brokerClient, cfg)

	log.Info("Pending worker starting")
	pendingWorker.Start(ctx)
}

func runEmailWorker(ctx context.Context, cfg *notificationConfig.Config, database *gorm.DB) {
	log := logger.L()
	brokerClient, err := broker.NewRabbitMQ(cfg.Broker)
	if err != nil {
		log.Panic("failed to connect to broker", zap.Error(err))
	}
	defer brokerClient.Close()

	notificationRepo := repository.NewNotificationRepository(database)
	campaignRepo := repository.NewCampaignRepository(database)
	emailWorker := worker.NewEmailWorker(notificationRepo, campaignRepo, brokerClient, cfg)

	log.Info("Email worker starting")
	emailWorker.Start(ctx)
}

func runCampaignWorker(ctx context.Context, cfg *notificationConfig.Config, database *gorm.DB) {
	log := logger.L()
	brokerClient, err := broker.NewRabbitMQ(cfg.Broker)
	if err != nil {
		log.Panic("failed to connect to broker", zap.Error(err))
	}
	defer brokerClient.Close()

	campaignRepo := repository.NewCampaignRepository(database)
	campaignWorker := worker.NewCampaignWorker(campaignRepo, brokerClient, cfg)

	log.Info("Campaign worker starting")
	campaignWorker.Start(ctx)
}

func runSchedulerWorker(ctx context.Context, cfg *notificationConfig.Config, database *gorm.DB) {
	log := logger.L()

	notificationRepo := repository.NewNotificationRepository(database)
	templateRepo := repository.NewTemplateRepository(database)
	scheduleRepo := repository.NewScheduleRepository(database)
	notificationSvc := service.NewNotificationService(notificationRepo, templateRepo, nil, nil, cfg)
	schedulerSvc := service.NewSchedulerService(scheduleRepo, templateRepo, notificationSvc)

	schedulerWorker := worker.NewSchedulerWorker(schedulerSvc)

	log.Info("Scheduler worker starting")
	schedulerWorker.Start(ctx)
}

func runWebhookWorker(ctx context.Context, cfg *notificationConfig.Config, database *gorm.DB) {
	log := logger.L()
	brokerClient, err := broker.NewRabbitMQ(cfg.Broker)
	if err != nil {
		log.Panic("failed to connect to broker", zap.Error(err))
	}
	defer brokerClient.Close()

	notificationRepo := repository.NewNotificationRepository(database)
	templateRepo := repository.NewTemplateRepository(database)
	notificationService := service.NewNotificationService(notificationRepo, templateRepo, nil, brokerClient, cfg)

	webhookWorker := worker.NewWebhookWorker(notificationService, brokerClient, cfg)

	log.Info("Webhook worker starting")
	webhookWorker.Start(ctx)
}
