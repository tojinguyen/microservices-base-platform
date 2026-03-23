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

		protected := v1.Group("/")
		protected.Use(GinAuthMiddleware(authenticator))
		{
			// Temp
			protected.GET("/me", func(c *gin.Context) {
				claims, _ := auth.CurrentUser(c.Request.Context())
				c.JSON(200, gin.H{"user_id": claims.UserID})
			})
		}
	}
}

func GinAuthMiddleware(a *auth.Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		tokenString := auth.ExtractToken(authHeader)

		if tokenString == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "Unauthorized"})
			return
		}

		claims, err := a.VerifyToken(tokenString)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "Invalid token"})
			return
		}

		ctx := auth.WithUser(c.Request.Context(), claims)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
