package route

import (
	"backend/pkg/auth"

	"github.com/gin-gonic/gin"
	"github.com/tojinguyen/identity/internal/handler"
)

func RegisterRoutes(r *gin.Engine, authHandler *handler.AuthHandler, authenticator *auth.Authenticator) {
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
	}
}
