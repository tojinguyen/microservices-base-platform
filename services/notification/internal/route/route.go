package route

import (
	"fmt"
	"time"

	"backend/pkg/ratelimit"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	notificationConfig "github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/handler"
)

func RegisterRoutes(r *gin.Engine, notificationHandler *handler.NotificationHandler, limiter *ratelimit.RateLimiter, rlCfg notificationConfig.RateLimitConfig) {
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	v1 := r.Group("/api/v1/notifications")
	v1.Use(limiter.GinMiddleware(ratelimit.ByIP))
	{
		v1.GET("", notificationHandler.ListNotifications)
		v1.GET("/health", notificationHandler.Health)
		v1.POST("/send",
			limiter.GinMiddlewareWithConfig(func(c *gin.Context) string {
				return fmt.Sprintf("rl:send:%s", c.ClientIP())
			}, rlCfg.SendLimit, time.Duration(rlCfg.SendWindowSecs)*time.Second),
			notificationHandler.SendNotification,
		)
		v1.POST("/webhooks/mailpit", notificationHandler.HandleMailpitWebhook)
	}
}
