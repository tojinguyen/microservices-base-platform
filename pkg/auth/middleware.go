package auth

import (
	"net/http"

	"backend/pkg/errors"
	"backend/pkg/response"

	"github.com/gin-gonic/gin"
)

func (a *Authenticator) RequireAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			authHeader := r.Header.Get("Authorization")
			tokenString := ExtractToken(authHeader)

			if tokenString == "" {
				response.Error(w, r, errors.Unauthorized("Missing or malformed Authorization header"))
				return
			}

			claims, err := a.VerifyToken(tokenString)
			if err != nil {
				response.Error(w, r, errors.Unauthorized("Invalid or expired token"))
				return
			}

			ctx := WithUser(r.Context(), claims)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func (a *Authenticator) GinRequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		tokenString := ExtractToken(authHeader)

		if tokenString == "" {
			response.Error(c.Writer, c.Request, errors.Unauthorized("Missing Authorization header"))
			c.Abort()
			return
		}

		claims, err := a.VerifyToken(tokenString)
		if err != nil {
			response.Error(c.Writer, c.Request, errors.Unauthorized("Invalid or expired token"))
			c.Abort()
			return
		}

		ctx := WithUser(c.Request.Context(), claims)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
