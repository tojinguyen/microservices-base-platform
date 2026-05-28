package storage

import (
	"context"
	"io"
)

// ObjectStorage abstracts the object-storage operations the transcoder needs:
// downloading raw video, uploading transcoded HLS segments, and presence checks.
type ObjectStorage interface {
	GetObject(ctx context.Context, key string) (io.ReadCloser, error)
	PutObject(ctx context.Context, key, contentType string, body io.Reader, size int64) error
	HeadObject(ctx context.Context, key string) (sizeBytes int64, exists bool, err error)
}
