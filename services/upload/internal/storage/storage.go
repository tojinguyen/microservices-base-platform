package storage

import (
	"context"
	"time"
)

type ObjectStorage interface {
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (url string, expiresAt time.Time, err error)
	HeadObject(ctx context.Context, key string) (sizeBytes int64, etag string, exists bool, err error)
	DeleteObject(ctx context.Context, key string) error
	PublicURL(key string) string
}
