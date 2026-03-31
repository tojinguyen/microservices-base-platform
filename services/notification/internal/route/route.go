package route

import (
	"github.com/gin-gonic/gin"
	"github.com/tojinguyen/notification/internal/handler"
)

func RegisterRoutes(r *gin.Engine, notificationHandler *handler.NotificationHandler) {
	v1 := r.Group("/api/v1/notifications")
	{
		v1.GET("/health", notificationHandler.Health)
	}
}
