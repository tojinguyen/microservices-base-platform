package route

import (
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"github.com/tojinguyen/notification/internal/handler"
)

func RegisterRoutes(r *gin.Engine, notificationHandler *handler.NotificationHandler) {
	// Swagger setup
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	v1 := r.Group("/api/v1/notifications")
	{
		v1.GET("/health", notificationHandler.Health)
		v1.POST("/send", notificationHandler.SendNotification)
	}
}
