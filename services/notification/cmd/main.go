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
	_ "github.com/tojinguyen/notification/docs"
	notificationConfig "github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/handler"
	"github.com/tojinguyen/notification/internal/repository"
	"github.com/tojinguyen/notification/internal/route"
	"github.com/tojinguyen/notification/internal/service"
	"github.com/tojinguyen/notification/migrations"
	"go.uber.org/zap"
)

// @title Notification Service API
// @version 1.0
// @description This is a notification service API
// @BasePath /api/v1/notifications

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
	templateRepo := repository.NewTemplateRepository(database)
	notificationService := service.NewNotificationService(notificationRepo, templateRepo)
	notificationHandler := handler.NewNotificationHandler(notificationService)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(logger.GinMiddleware())
	route.RegisterRoutes(r, notificationHandler)

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

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeGrace)*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("server forced to shutdown", zap.Error(err))
	}
}
