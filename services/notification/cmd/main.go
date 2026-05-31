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

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/gin-gonic/gin"
	_ "github.com/tojinguyen/notification/docs"
	notificationConfig "github.com/tojinguyen/notification/internal/config"
	notifgrpc "github.com/tojinguyen/notification/internal/grpc"
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
// @BasePath /api/v1

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

	defer trace.Setup(ctx, log, "notification-service", cfg.Otel.Enabled, cfg.Otel.ExporterEndpoint)()

	log.Info("Starting notification service", zap.String("mode", cfg.AppMode))

	switch cfg.AppMode {
	case notificationConfig.ModeWorkerEmail:
		runEmailWorker(ctx, cfg, database)
	case notificationConfig.ModeWorkerWebhook:
		runWebhookWorker(ctx, cfg, database)
	case notificationConfig.ModeWorkerScheduler:
		runSchedulerWorker(ctx, cfg, database)
	case notificationConfig.ModeWorkerCampaign:
		runCampaignWorker(ctx, cfg, database)
	case notificationConfig.ModeWorkerOutbox:
		runOutboxWorker(ctx, cfg, database)
	case notificationConfig.ModeWorkerDLQ:
		runDLQWorker(ctx, cfg, database)
	case notificationConfig.ModeAPI:
		runAPI(ctx, cfg, database)
	default:
		log.Error("Unknown app mode, falling back to API mode", zap.String("mode", cfg.AppMode))
		return
	}
}

// startMetricsServer exposes the Prometheus /metrics endpoint on a dedicated port for worker
// pods that have no main HTTP server. Port 9090 is the conventional Prometheus scrape port.
func startMetricsServer(ctx context.Context) {
	log := logger.L()
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{Addr: ":9090", Handler: mux}

	go func() {
		log.Info("metrics server starting", zap.Int("port", 9090))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("metrics server failed", zap.Error(err))
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
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
	outboxRepo := repository.NewOutboxRepository(database)
	templateRepo := repository.NewTemplateRepository(database)
	prefRepo := repository.NewPreferenceRepository(database)
	prefSvc := service.NewPreferenceService(prefRepo, cache)
	notificationService := service.NewNotificationService(database, notificationRepo, outboxRepo, templateRepo, prefSvc, brokerClient, cfg)

	scheduleRepo := repository.NewScheduleRepository(database)
	schedulerSvc := service.NewSchedulerService(scheduleRepo, templateRepo, notificationService)

	campaignRepo := repository.NewCampaignRepository(database)
	campaignSvc := service.NewCampaignService(campaignRepo)

	dlqRepo := repository.NewDLQRepository(database)
	dlqSvc := service.NewDLQService(dlqRepo, notificationRepo, brokerClient, cfg)

	notificationHandler := handler.NewNotificationHandler(notificationService)
	preferenceHandler := handler.NewPreferenceHandler(prefSvc)
	scheduleHandler := handler.NewScheduleHandler(schedulerSvc)
	campaignHandler := handler.NewCampaignHandler(campaignSvc)
	dlqHandler := handler.NewDLQHandler(dlqSvc)

	if err := notificationService.SeedTemplates(context.Background()); err != nil {
		log.Error("failed to seed notification templates", zap.Error(err))
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(logger.GinMiddleware())
	r.Use(trace.TracerMiddleware("notification-service")) // OTel span for each HTTP request
	route.RegisterRoutes(r, notificationHandler, preferenceHandler, scheduleHandler, campaignHandler, dlqHandler, limiter, cfg.RateLimit)

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

func runEmailWorker(ctx context.Context, cfg *notificationConfig.Config, database *gorm.DB) {
	log := logger.L()
	startMetricsServer(ctx)

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
	startMetricsServer(ctx)

	brokerClient, err := broker.NewRabbitMQ(cfg.Broker)
	if err != nil {
		log.Panic("failed to connect to broker", zap.Error(err))
	}
	defer brokerClient.Close()

	identityAddr := cfg.IdentityGRPCAddr
	identityClient, err := notifgrpc.NewIdentityClient(identityAddr)
	if err != nil {
		log.Panic("failed to dial identity gRPC", zap.String("addr", identityAddr), zap.Error(err))
	}
	defer identityClient.Close()

	campaignRepo := repository.NewCampaignRepository(database)
	campaignWorker := worker.NewCampaignWorker(campaignRepo, brokerClient, cfg, identityClient)

	log.Info("Campaign worker starting", zap.String("identity_grpc", identityAddr))
	campaignWorker.Start(ctx)
}

func runSchedulerWorker(ctx context.Context, cfg *notificationConfig.Config, database *gorm.DB) {
	log := logger.L()
	startMetricsServer(ctx)

	notificationRepo := repository.NewNotificationRepository(database)
	templateRepo := repository.NewTemplateRepository(database)
	scheduleRepo := repository.NewScheduleRepository(database)
	notificationSvc := service.NewNotificationService(database, notificationRepo, nil, templateRepo, nil, nil, cfg)
	schedulerSvc := service.NewSchedulerService(scheduleRepo, templateRepo, notificationSvc)

	schedulerWorker := worker.NewSchedulerWorker(schedulerSvc)

	log.Info("Scheduler worker starting")
	schedulerWorker.Start(ctx)
}

func runWebhookWorker(ctx context.Context, cfg *notificationConfig.Config, database *gorm.DB) {
	log := logger.L()
	startMetricsServer(ctx)

	brokerClient, err := broker.NewRabbitMQ(cfg.Broker)
	if err != nil {
		log.Panic("failed to connect to broker", zap.Error(err))
	}
	defer brokerClient.Close()

	notificationRepo := repository.NewNotificationRepository(database)
	templateRepo := repository.NewTemplateRepository(database)
	notificationService := service.NewNotificationService(database, notificationRepo, nil, templateRepo, nil, brokerClient, cfg)

	webhookWorker := worker.NewWebhookWorker(notificationService, brokerClient, cfg)

	log.Info("Webhook worker starting")
	webhookWorker.Start(ctx)
}

func runOutboxWorker(ctx context.Context, cfg *notificationConfig.Config, database *gorm.DB) {
	log := logger.L()
	startMetricsServer(ctx)

	brokerClient, err := broker.NewRabbitMQ(cfg.Broker)
	if err != nil {
		log.Panic("failed to connect to broker", zap.Error(err))
	}
	defer brokerClient.Close()

	outboxRepo := repository.NewOutboxRepository(database)
	notificationRepo := repository.NewNotificationRepository(database)
	outboxWorker := worker.NewOutboxWorker(database, outboxRepo, notificationRepo, brokerClient, cfg)

	log.Info("Outbox worker starting")
	outboxWorker.Start(ctx)
}

func runDLQWorker(ctx context.Context, cfg *notificationConfig.Config, database *gorm.DB) {
	log := logger.L()
	startMetricsServer(ctx)

	brokerClient, err := broker.NewRabbitMQ(cfg.Broker)
	if err != nil {
		log.Panic("failed to connect to broker", zap.Error(err))
	}
	defer brokerClient.Close()

	dlqRepo := repository.NewDLQRepository(database)
	dlqWorker := worker.NewDLQWorker(dlqRepo, brokerClient, cfg)

	log.Info("DLQ worker starting")
	dlqWorker.Start(ctx)
}
