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

func RegisterRoutes(
	r *gin.Engine,
	notificationHandler *handler.NotificationHandler,
	preferenceHandler *handler.PreferenceHandler,
	scheduleHandler *handler.ScheduleHandler,
	campaignHandler *handler.CampaignHandler,
	dlqHandler *handler.DLQHandler,
	limiter *ratelimit.RateLimiter,
	rlCfg notificationConfig.RateLimitConfig,
) {
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
		v1.POST("/schedule", notificationHandler.ScheduleNotification)
		v1.POST("/webhooks/mailpit", notificationHandler.HandleMailpitWebhook)
	}

	users := r.Group("/api/v1/users")
	users.Use(limiter.GinMiddleware(ratelimit.ByIP))
	{
		users.GET("/:id/notification-preferences", preferenceHandler.GetPreferences)
		users.PUT("/:id/notification-preferences", preferenceHandler.UpsertPreferences)

		users.GET("/:id/notification-schedules", scheduleHandler.GetSchedules)
		users.PUT("/:id/notification-schedules", scheduleHandler.UpsertSchedule)
		users.DELETE("/:id/notification-schedules", scheduleHandler.DeleteSchedule)
	}

	// Admin endpoints — in production, protect with API key or internal service token middleware.
	admin := r.Group("/admin")
	admin.Use(limiter.GinMiddleware(ratelimit.ByIP))
	{
		campaigns := admin.Group("/campaigns")
		{
			campaigns.POST("", campaignHandler.CreateCampaign)
			campaigns.GET("/:id/stats", campaignHandler.GetCampaignStats)
		}

		dlq := admin.Group("/dlq")
		{
			dlq.GET("/messages", dlqHandler.ListMessages)
			dlq.POST("/replay/:id", dlqHandler.ReplayMessage)
		}
	}
}
