package ffmpeg

import (
	"context"

	"github.com/tojinguyen/transcoder/internal/dto"
)

type RenditionPath struct {
	Name         string
	PlaylistPath string // absolute path to <rendition>.m3u8
	SegmentDir   string // absolute directory holding .ts segments
	Width        int
	Height       int
	BitrateKbps  int
	AudioOnly    bool
}

type TranscodeResult struct {
	DurationSeconds float64
	OutputSizeBytes int64
	Renditions      []RenditionPath
}

type Transcoder interface {
	Transcode(ctx context.Context, input, outputDir string, renditions []dto.RenditionConfig) (*TranscodeResult, error)
}
