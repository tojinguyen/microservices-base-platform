package dto

import "time"

// VideoUploadedEvent — payload from upload-service consumed by transcoder.
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

type RenditionOutput struct {
	Name        string `json:"name"`
	Playlist    string `json:"playlist"`
	BitrateKbps int    `json:"bitrate_kbps"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
}

// VideoTranscodedEvent — emitted on success.
type VideoTranscodedEvent struct {
	VideoID         string            `json:"video_id"`
	JobID           string            `json:"job_id"`
	HLSBasePath     string            `json:"hls_base_path"`
	MasterPlaylist  string            `json:"master_playlist"`
	Renditions      []RenditionOutput `json:"renditions"`
	DurationSeconds float64           `json:"duration_seconds"`
	OutputSizeBytes int64             `json:"output_size_bytes"`
	TranscodedAt    time.Time         `json:"transcoded_at"`
}

// VideoTranscodeFailedEvent — emitted on permanent failure (dead).
type VideoTranscodeFailedEvent struct {
	VideoID      string    `json:"video_id"`
	JobID        string    `json:"job_id"`
	ErrorMessage string    `json:"error_message"`
	RetryCount   int       `json:"retry_count"`
	FailedAt     time.Time `json:"failed_at"`
}
