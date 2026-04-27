package route

import (
	"backend/pkg/auth"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tojinguyen/identity/internal/handler"
	"github.com/tojinguyen/identity/internal/middleware"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	_ "github.com/tojinguyen/identity/docs"
)

func RegisterRoutes(r *gin.Engine, authHandler *handler.AuthHandler, authenticator *auth.Authenticator) {
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	r.Use(middleware.PrometheusMiddleware())

	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	v1 := r.Group("/api/v1/auth")
	{
		v1.POST("/register", authHandler.Register)
		v1.POST("/login", authHandler.Login)
		v1.POST("/refresh", authHandler.RefreshToken)

		v1.GET("/google/login", authHandler.GoogleLogin)
		v1.GET("/google/callback", authHandler.GoogleCallback)
	}

	profile := r.Group("/api/v1/profile")
	profile.Use(authenticator.GinRequireAuth())
	{
		profile.GET("/", authHandler.GetProfile)
		v1.POST("/logout", authHandler.Logout)
	}
}
