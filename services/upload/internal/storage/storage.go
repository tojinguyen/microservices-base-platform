package storage

import (
	"context"
	"time"
)

type CompletedPart struct {
	PartNumber int32
	ETag       string // without surrounding quotes
}

type PartInfo struct {
	PartNumber   int32
	ETag         string
	SizeBytes    int64
	LastModified time.Time
}

type ObjectStorage interface {
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (url string, expiresAt time.Time, err error)
	HeadObject(ctx context.Context, key string) (sizeBytes int64, etag string, exists bool, err error)
	DeleteObject(ctx context.Context, key string) error
	PublicURL(key string) string

	CreateMultipartUpload(ctx context.Context, objectKey, mimeType string) (uploadID string, err error)
	GeneratePresignedPartURL(ctx context.Context, objectKey, uploadID string, partNumber int32, ttl time.Duration) (url string, expiresAt time.Time, err error)
	CompleteMultipartUpload(ctx context.Context, objectKey, uploadID string, parts []CompletedPart) (etag string, err error)
	AbortMultipartUpload(ctx context.Context, objectKey, uploadID string) error
	ListMultipartParts(ctx context.Context, objectKey, uploadID string) ([]PartInfo, error)
}
