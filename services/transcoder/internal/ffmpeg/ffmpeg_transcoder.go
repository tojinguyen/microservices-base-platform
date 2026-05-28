package ffmpeg

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"backend/pkg/logger"

	"github.com/tojinguyen/transcoder/internal/dto"
	"go.uber.org/zap"
)

type FFmpegTranscoder struct {
	binary          string
	segmentDuration int
}

func NewFFmpegTranscoder(binary string, segmentDuration int) *FFmpegTranscoder {
	if binary == "" {
		binary = "ffmpeg"
	}
	if segmentDuration <= 0 {
		segmentDuration = 6
	}
	return &FFmpegTranscoder{binary: binary, segmentDuration: segmentDuration}
}

func (t *FFmpegTranscoder) Transcode(ctx context.Context, input, outputDir string, renditions []dto.RenditionConfig) (*TranscodeResult, error) {
	log := logger.L()

	for _, r := range renditions {
		dir := filepath.Join(outputDir, r.Name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}

	args := t.buildArgs(input, outputDir, renditions)
	cmd := exec.CommandContext(ctx, t.binary, args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	log.Info("ffmpeg starting", zap.String("input", input), zap.Int("renditions", len(renditions)))
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg start: %w", err)
	}

	go streamLog(stderr, log)

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("ffmpeg run: %w", err)
	}

	return collectResult(outputDir, renditions, t.segmentDuration)
}

func (t *FFmpegTranscoder) buildArgs(input, outputDir string, renditions []dto.RenditionConfig) []string {
	args := []string{"-y", "-i", input}
	segDur := strconv.Itoa(t.segmentDuration)

	for _, r := range renditions {
		playlistPath := filepath.Join(outputDir, r.Name, r.Name+".m3u8")
		segmentPattern := filepath.Join(outputDir, r.Name, "%04d.ts")

		if r.AudioOnly {
			args = append(args,
				"-map", "0:a:0",
				"-c:a", "aac",
				"-b:a", fmt.Sprintf("%dk", r.AudioBitrateK),
				"-ac", strconv.Itoa(r.AudioChannels),
				"-vn",
				"-f", "hls",
				"-hls_time", segDur,
				"-hls_playlist_type", "vod",
				"-hls_segment_filename", segmentPattern,
				playlistPath,
			)
			continue
		}

		args = append(args,
			"-map", "0:v:0", "-map", "0:a:0?",
			"-c:v", "libx264",
			"-crf", "23",
			"-preset", "fast",
			"-sc_threshold", "0",
			"-g", "48", "-keyint_min", "48",
			"-b:v", fmt.Sprintf("%dk", r.VideoBitrateK),
			"-maxrate", fmt.Sprintf("%dk", r.MaxRateK),
			"-bufsize", fmt.Sprintf("%dk", r.BufSizeK),
			"-vf", fmt.Sprintf("scale=%d:-2", r.Width),
			"-c:a", "aac",
			"-b:a", fmt.Sprintf("%dk", r.AudioBitrateK),
			"-ac", strconv.Itoa(r.AudioChannels),
			"-f", "hls",
			"-hls_time", segDur,
			"-hls_playlist_type", "vod",
			"-hls_segment_filename", segmentPattern,
			playlistPath,
		)
	}
	return args
}

func streamLog(r io.Reader, log *zap.Logger) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		log.Debug("ffmpeg", zap.String("line", line))
	}
}

func collectResult(outputDir string, renditions []dto.RenditionConfig, segDur int) (*TranscodeResult, error) {
	result := &TranscodeResult{}

	for _, r := range renditions {
		playlistPath := filepath.Join(outputDir, r.Name, r.Name+".m3u8")
		segDir := filepath.Join(outputDir, r.Name)

		bitrate := r.VideoBitrateK
		if r.AudioOnly {
			bitrate = r.AudioBitrateK
		}

		result.Renditions = append(result.Renditions, RenditionPath{
			Name:         r.Name,
			PlaylistPath: playlistPath,
			SegmentDir:   segDir,
			Width:        r.Width,
			Height:       r.Height,
			BitrateKbps:  bitrate,
			AudioOnly:    r.AudioOnly,
		})
	}

	var totalSize int64
	err := filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk output dir: %w", err)
	}
	result.OutputSizeBytes = totalSize

	// Derive duration from the first video rendition's playlist.
	for _, rp := range result.Renditions {
		if rp.AudioOnly {
			continue
		}
		d, err := parsePlaylistDuration(rp.PlaylistPath)
		if err == nil {
			result.DurationSeconds = d
			break
		}
	}
	if result.DurationSeconds == 0 && len(result.Renditions) > 0 {
		// fallback: estimate from segment count * targetDur on first rendition
		count, _ := countSegments(result.Renditions[0].SegmentDir)
		result.DurationSeconds = float64(count * segDur)
	}

	return result, nil
}

func parsePlaylistDuration(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	var total float64
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#EXTINF:") {
			v := strings.TrimSuffix(strings.TrimPrefix(line, "#EXTINF:"), ",")
			if d, err := strconv.ParseFloat(v, 64); err == nil {
				total += d
			}
		}
	}
	return total, scanner.Err()
}

func countSegments(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".ts") {
			count++
		}
	}
	return count, nil
}
