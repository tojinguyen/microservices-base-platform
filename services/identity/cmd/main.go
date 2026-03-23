package cmd

import (
	"backend/pkg/auth"
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
	identity_config "github.com/tojinguyen/identity/internal/config"
	"github.com/tojinguyen/identity/internal/handler"
	"github.com/tojinguyen/identity/internal/repository"
	"github.com/tojinguyen/identity/internal/route"
	"github.com/tojinguyen/identity/internal/service"
	"go.uber.org/zap"
)

func main() {
	logger.Init("identity-service")
	log := logger.L()

	cfg := &identity_config.Config{}
	if err := config.Load(cfg); err != nil {
		log.Panic("Failed to load identity configuration")
	}

	database, _ := db.New(cfg.Database)
	database.AutoMigrate()

	authenticator := auth.New(cfg.JWT)
	userRepo := repository.NewUserRepository(database)
	authService := service.NewAuthService(userRepo, authenticator, cfg.GoogleOAuth.ClientID, cfg.GoogleOAuth.ClientSecret, cfg.GoogleOAuth.RedirectURL)
	authHandler := handler.NewAuthHandler(authService)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	r.Use(gin.Recovery())
	r.Use(logger.GinMiddleware())

	route.RegisterRoutes(r, authHandler, authenticator)

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

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeGrace)*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown", zap.Error(err))
	}
}
