package main

import (
	"backend/pkg/auth"
	"backend/pkg/config"
	"backend/pkg/db"
	"backend/pkg/logger"
	"backend/pkg/redis"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	identity_config "github.com/tojinguyen/identity/internal/config"
	identitygrpc "github.com/tojinguyen/identity/internal/grpc"
	"github.com/tojinguyen/identity/internal/handler"
	"github.com/tojinguyen/identity/internal/repository"
	"github.com/tojinguyen/identity/internal/route"
	"github.com/tojinguyen/identity/internal/service"
	"github.com/tojinguyen/identity/migrations"
	"go.uber.org/zap"
)

// @title           Identity Service API
// @version         1.0
// @description     This is the API documentation for the Identity Service.
// @host            localhost
// @BasePath        /api/v1
// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name Authorization
func main() {
	logger.Init("identity-service", "dev")
	log := logger.L()

	cfg := &identity_config.Config{}
	if err := config.Load(cfg); err != nil {
		log.Panic("Failed to load identity configuration")
	}

	database, err := db.New(cfg.Database)
	if err != nil {
		log.Panic("Failed to connect to database", zap.Error(err))
	}

	sqlDB, err := database.DB()
	if err != nil {
		log.Panic("Failed to get sql.DB from gorm", zap.Error(err))
	}

	if err := db.RunMigrations(sqlDB, migrations.FS, "."); err != nil {
		log.Panic("Failed to run database migrations", zap.Error(err))
	}

	redisClient, err := redis.New(cfg.Redis)
	if err != nil {
		log.Panic("Failed to connect to redis", zap.Error(err))
	}
	cache := redis.NewCache(redisClient)

	authenticator := auth.New(cfg.JWT)
	userRepo := repository.NewUserRepository(database)
	authService := service.NewAuthService(userRepo, authenticator, cache, cfg.GoogleOAuth.ClientID, cfg.GoogleOAuth.ClientSecret, cfg.GoogleOAuth.RedirectURL)
	authHandler := handler.NewAuthHandler(authService)

	seedService := service.NewSeedService(userRepo)
	seedHandler := handler.NewSeedHandler(seedService)

	// Start gRPC server for inter-service streaming (e.g. campaign user stream).
	grpcPort := cfg.GRPCPort
	grpcServer := identitygrpc.NewServer(userRepo)
	go func() {
		if err := identitygrpc.ListenAndServe(grpcServer, grpcPort); err != nil {
			log.Fatal("gRPC server failed", zap.Error(err))
		}
	}()

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	r.Use(gin.Recovery())
	r.Use(logger.GinMiddleware())

	route.RegisterRoutes(r, authHandler, seedHandler, authenticator)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.ServerPort),
		Handler: r,
	}

	go func() {
		log.Info("Server starting", zap.Int("port", cfg.ServerPort))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("failed to run server", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	grpcServer.GracefulStop()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeGrace)*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown", zap.Error(err))
	}
}
