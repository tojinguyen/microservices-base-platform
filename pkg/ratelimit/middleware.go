package ratelimit

import (
	"fmt"
	"strconv"
	"time"

	"backend/pkg/auth"
	"backend/pkg/errors"
	"backend/pkg/response"

	"github.com/gin-gonic/gin"
)

type KeyFunc func(c *gin.Context) string

func ByIP(c *gin.Context) string {
	return fmt.Sprintf("rl:ip:%s", c.ClientIP())
}

func ByUserID(c *gin.Context) string {
	if claims, ok := auth.CurrentUser(c.Request.Context()); ok {
		return fmt.Sprintf("rl:user:%s", claims.UserID)
	}
	return fmt.Sprintf("rl:ip:%s", c.ClientIP())
}

func (r *RateLimiter) GinMiddleware(keyFn KeyFunc) gin.HandlerFunc {
	return r.GinMiddlewareWithConfig(keyFn, r.cfg.Limit, r.cfg.Window)
}

func (r *RateLimiter) GinMiddlewareWithConfig(keyFn KeyFunc, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := keyFn(c)

		result, err := r.allow(c.Request.Context(), key, limit, window)
		if err != nil {
			c.Next()
			return
		}

		resetSecs := strconv.FormatInt(int64(result.ResetIn.Seconds()), 10)
		c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
		c.Header("X-RateLimit-Reset", resetSecs)

		if !result.Allowed {
			c.Header("Retry-After", resetSecs)
			response.Error(c.Writer, c.Request, errors.TooManyRequests(""))
			c.Abort()
			return
		}

		c.Next()
	}
}
