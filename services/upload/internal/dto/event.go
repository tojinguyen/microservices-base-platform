package dto

import "time"

type VideoUploadedEvent struct {
	VideoID    string     `json:"video_id"`
	ObjectKey  string     `json:"object_key"`
	Bucket     string     `json:"bucket"`
	MimeType   string     `json:"mime_type"`
	SizeBytes  *int64     `json:"size_bytes,omitempty"`
	Etag       string     `json:"etag,omitempty"`
	OwnerID    *string    `json:"owner_id,omitempty"`
	UploadedAt *time.Time `json:"uploaded_at,omitempty"`
	StorageURL string     `json:"storage_url,omitempty"`
}
