package route

import (
	"backend/pkg/auth"
	"net/http"

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
		protected.Use(GinAuthAdapter(authenticator))
		{
			// Temp
			protected.GET("/me", func(c *gin.Context) {
				claims, _ := auth.CurrentUser(c.Request.Context())
				c.JSON(200, gin.H{"user_id": claims.UserID})
			})
		}
	}
}

func GinAuthAdapter(a *auth.Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			c.Request = r
		})

		a.RequireAuth()(next).ServeHTTP(c.Writer, c.Request)

		if !nextCalled {
			c.Abort()
		}
	}
}
