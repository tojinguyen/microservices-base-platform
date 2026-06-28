package route

import (
	"time"

	"backend/pkg/ratelimit"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	uploadConfig "github.com/tojinguyen/upload/internal/config"
	"github.com/tojinguyen/upload/internal/handler"
)

func RegisterRoutes(
	r *gin.Engine,
	videoHandler *handler.VideoHandler,
	sessionHandler *handler.UploadSessionHandler,
	limiter *ratelimit.RateLimiter,
	rlCfg uploadConfig.RateLimitConfig,
) {
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	rl := limiter.GinMiddlewareWithConfig(
		ratelimit.ByIP,
		rlCfg.Limit,
		time.Duration(rlCfg.WindowSecs)*time.Second,
	)

	v1 := r.Group("/api/v1/videos")
	v1.Use(rl)
	{
		v1.POST("", videoHandler.InitUpload)
		v1.GET("", videoHandler.ListVideos)
		v1.GET("/:id", videoHandler.GetVideo)
		v1.POST("/:id/complete", videoHandler.CompleteUpload)
		v1.POST("/:id/abort", videoHandler.AbortUpload)
	}

	uploads := r.Group("/api/v1/uploads")
	uploads.Use(rl)
	{
		uploads.POST("/sessions", sessionHandler.InitChunkedUpload)
		uploads.GET("/sessions/:id", sessionHandler.GetSession)
		uploads.POST("/sessions/:id/parts/:number/url", sessionHandler.GetPartURL)
		uploads.POST("/sessions/:id/parts/:number/confirm", sessionHandler.ConfirmPart)
		uploads.POST("/sessions/:id/complete", sessionHandler.CompleteSession)
		uploads.POST("/sessions/:id/abort", sessionHandler.AbortSession)
	}
}
