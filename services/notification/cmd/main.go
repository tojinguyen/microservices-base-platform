package main

import (
	"backend/pkg/broker"
	"backend/pkg/config"
	"backend/pkg/db"
	"backend/pkg/logger"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	notificationConfig "github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/consumer"
	"github.com/tojinguyen/notification/internal/handler"
	"github.com/tojinguyen/notification/internal/repository"
	"github.com/tojinguyen/notification/internal/route"
	"github.com/tojinguyen/notification/internal/service"
	"github.com/tojinguyen/notification/migrations"
	"go.uber.org/zap"
)

func main() {
	if err := logger.Init("notification-service"); err != nil {
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

	if err := db.RunMigrations(sqlDB, migrations.FS, "."); err != nil {
		log.Panic("failed to run database migrations", zap.Error(err))
	}

	brokerClient, err := broker.NewRabbitMQ(cfg.Broker)
	if err != nil {
		log.Panic("failed to connect to broker", zap.Error(err))
	}
	defer brokerClient.Close()

	notificationRepo := repository.NewNotificationRepository(database)
	emailSender := service.NewSMTPSender(cfg.SMTP.Host, cfg.SMTP.Port, cfg.SMTP.Username, cfg.SMTP.Password, cfg.SMTP.From)
	notificationService := service.NewNotificationService(notificationRepo, emailSender)
	notificationConsumer := consumer.NewNotificationConsumer(brokerClient, notificationService, cfg.Queue.NotificationEvents)

	notificationHandler := handler.NewNotificationHandler()

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(logger.GinMiddleware())
	route.RegisterRoutes(r, notificationHandler)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.ServerPort),
		Handler: r,
	}

	consumerCtx, cancelConsumer := context.WithCancel(context.Background())
	defer cancelConsumer()

	if err := notificationConsumer.Start(consumerCtx); err != nil {
		log.Panic("failed to start notification consumer", zap.Error(err))
	}
	log.Info("notification consumer started", zap.String("queue", cfg.Queue.NotificationEvents))

	go func() {
		log.Info("server starting", zap.Int("port", cfg.ServerPort))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("failed to run server", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	cancelConsumer()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeGrace)*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("server forced to shutdown", zap.Error(err))
	}
}
