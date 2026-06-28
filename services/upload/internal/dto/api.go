package dto

import "time"

type InitUploadRequest struct {
	Title       string `json:"title" binding:"required,max=255"`
	Description string `json:"description"`
	MimeType    string `json:"mime_type" binding:"required"`
	SizeBytes   int64  `json:"size_bytes" binding:"required,min=1"`
}

type InitUploadResponse struct {
	VideoID      string    `json:"video_id"`
	ObjectKey    string    `json:"object_key"`
	Bucket       string    `json:"bucket"`
	UploadURL    string    `json:"upload_url"`
	UploadMethod string    `json:"upload_method"`
	ExpiresAt    time.Time `json:"expires_at"`
	Status       string    `json:"status"`
}

type CompleteUploadResponse struct {
	VideoID    string     `json:"video_id"`
	Status     string     `json:"status"`
	SizeBytes  *int64     `json:"size_bytes,omitempty"`
	Etag       string     `json:"etag,omitempty"`
	UploadedAt *time.Time `json:"uploaded_at,omitempty"`
}

type AbortUploadResponse struct {
	VideoID string `json:"video_id"`
	Status  string `json:"status"`
}

type VideoResponse struct {
	VideoID         string     `json:"video_id"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	ObjectKey       string     `json:"object_key"`
	Bucket          string     `json:"bucket"`
	MimeType        string     `json:"mime_type"`
	SizeBytes       *int64     `json:"size_bytes,omitempty"`
	Status          string     `json:"status"`
	StorageURL      string     `json:"storage_url,omitempty"`
	UploadExpiresAt *time.Time `json:"upload_expires_at,omitempty"`
	UploadedAt      *time.Time `json:"uploaded_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type ListVideosRequest struct {
	Status   string `form:"status"`
	Page     int    `form:"page,default=1" binding:"min=1"`
	PageSize int    `form:"page_size,default=20" binding:"min=1,max=100"`
}

type ListVideosResponse struct {
	Videos   []VideoResponse `json:"videos"`
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

// ── Chunked upload DTOs ────────────────────────────────────────────────────

type InitChunkedUploadRequest struct {
	VideoID   string `json:"video_id" binding:"required,uuid"`
	MimeType  string `json:"mime_type" binding:"required"`
	SizeBytes int64  `json:"size_bytes" binding:"required,min=1"`
}

type InitChunkedUploadResponse struct {
	SessionID   string    `json:"session_id"`
	VideoID     string    `json:"video_id"`
	TotalParts  int       `json:"total_parts"`
	PartSize    int64     `json:"part_size_bytes"`
	TotalSize   int64     `json:"total_size_bytes"`
	Status      string    `json:"status"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type PartStateResponse struct {
	PartNumber int    `json:"part_number"`
	Status     string `json:"status"`
	Etag       string `json:"etag,omitempty"`
}

type GetSessionResponse struct {
	SessionID      string              `json:"session_id"`
	VideoID        string              `json:"video_id"`
	TotalParts     int                 `json:"total_parts"`
	PartSizeBytes  int64               `json:"part_size_bytes"`
	TotalSizeBytes int64               `json:"total_size_bytes"`
	Status         string              `json:"status"`
	ExpiresAt      time.Time           `json:"expires_at"`
	Parts          []PartStateResponse `json:"parts"`
}

type GetPartURLResponse struct {
	SessionID  string    `json:"session_id"`
	PartNumber int       `json:"part_number"`
	UploadURL  string    `json:"upload_url"`
	Method     string    `json:"method"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type ConfirmPartRequest struct {
	Etag string `json:"etag" binding:"required"`
}

type ConfirmPartResponse struct {
	SessionID     string `json:"session_id"`
	PartNumber    int    `json:"part_number"`
	Status        string `json:"status"`
	UploadedParts int    `json:"uploaded_parts"`
	TotalParts    int    `json:"total_parts"`
}

type CompleteChunkedUploadResponse struct {
	SessionID  string     `json:"session_id"`
	VideoID    string     `json:"video_id"`
	Status     string     `json:"status"`
	Etag       string     `json:"etag,omitempty"`
	SizeBytes  *int64     `json:"size_bytes,omitempty"`
	UploadedAt *time.Time `json:"uploaded_at,omitempty"`
}

type AbortSessionResponse struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}
