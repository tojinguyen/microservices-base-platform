# Transcoder Service — Design Document

> Service mới: `transcoder-service` — downstream consumer của `upload-service`. Nhận event `video.uploaded`, chạy FFmpeg để tạo HLS adaptive bitrate streaming, upload segments lên MinIO/S3, và phát event `video.transcoded` cho các service tiếp theo (player API, search indexer, thumbnail generator…).

---

## 1. Context & Mục tiêu

Upload-service đã hoàn chỉnh flow ingest: client upload raw video lên MinIO, service xác nhận và phát event `video.uploaded` lên exchange `video.events`. Transcoder-service là **consumer đầu tiên** của event đó.

Nhiệm vụ:

- Consume event `video.uploaded` từ RabbitMQ queue `transcoder.video.uploaded`.
- Download raw video từ MinIO/S3 về local tmp dir.
- Chạy **FFmpeg** để tạo HLS multi-bitrate (360p / 720p / 1080p + audio-only).
- Upload toàn bộ HLS output (`.m3u8` playlists + `.ts` segments) lên MinIO/S3.
- Cập nhật bảng `transcoding_jobs` (trạng thái, đường dẫn output, thống kê).
- Phát event `video.transcoded` hoặc `video.transcode_failed` lên exchange `video.events`.
- Dọn dẹp file tạm sau mỗi job.

**Out of scope (phase 1):**

- Thumbnail / preview frame extraction — service riêng (thumbnail-service).
- DASH (MPEG-DASH) output — thêm ở phase 2.
- Subtitle / caption processing.
- DRM (Widevine / FairPlay).
- Live streaming (RTMP ingest).
- Progress reporting real-time qua WebSocket.

---

## 2. Quyết định thiết kế cốt lõi

| Quyết định | Lựa chọn | Lý do |
|---|---|---|
| Trigger cơ chế | **RabbitMQ event** (`video.uploaded`) — không polling DB | Loose coupling; upload-service và transcoder-service hoàn toàn độc lập. Thêm consumer khác mà không sửa upload-service. |
| Transcoding engine | **FFmpeg** gọi qua `os/exec` | Tiêu chuẩn ngành, hỗ trợ hầu hết codec. Go không có FFmpeg binding mature ổn định → dùng `exec.CommandContext`. |
| Output format | **HLS** với master playlist + rendition playlists | Browser/mobile đều hỗ trợ (Safari native, Chrome/Firefox qua HLS.js). Tương thích CDN, scrubbing tốt. |
| Số renditions | **3 video + 1 audio-only**: 360p/500kbps, 720p/2.5Mbps, 1080p/5Mbps + audio/128kbps | Đủ cho 90% use case; bỏ 4K phase 1 để tránh chi phí CPU/storage quá cao. |
| Segment duration | **6 giây** | Apple HLS best practice. Cân bằng giữa seek latency và số file `.ts`. |
| Concurrency | **Worker pool** `TRANSCODER_CONCURRENCY` (mặc định 2) | Transcoding cực kỳ CPU-intensive; giới hạn goroutine để không OOM. |
| Storage layout | `hls/{video_id}/{rendition}/` cho segments; `hls/{video_id}/master.m3u8` | Namespace rõ ràng, dễ áp CDN prefix và lifecycle policy riêng. |
| APP_MODE | `worker` (một mode duy nhất phase 1) | Service này không có REST API riêng ở phase 1. |
| Retry | **RabbitMQ DLQ** + `retry_count` trong DB | Khi FFmpeg lỗi: nack → message vào DLQ sau N lần retry. Không block queue chính. |
| Outbox | Không (phase 1) | Publish event sau khi UPDATE DB; nếu crash sau UPDATE nhưng trước publish → janitor-style reconciler phase 2. |
| Auth | Không có — internal service, không expose HTTP ra ngoài | Giao tiếp chỉ qua RabbitMQ + MinIO. |

---

## 3. Kiến trúc & Luồng xử lý

```
┌────────────────┐  video.uploaded   ┌──────────────────────────┐
│ upload-service │ ───────────────▶  │   transcoder-service     │
│                │  (video.events    │   (worker mode only)     │
│                │   exchange)       │                          │
└────────────────┘                   └─────────────┬────────────┘
                                                   │
                              ┌────────────────────▼──────────────────────┐
                              │  1. Ack message & INSERT transcoding_job   │
                              │  2. PresignGetObject → download raw video  │
                              │     (tmp/<job_id>/input.mp4)               │
                              │  3. FFmpeg → HLS segments                  │
                              │     (tmp/<job_id>/hls/360p/…)              │
                              │  4. Upload all .m3u8 + .ts → MinIO         │
                              │     (hls/<video_id>/…)                     │
                              │  5. UPDATE job: status=completed           │
                              │  6. Publish video.transcoded               │
                              │  7. rm -rf tmp/<job_id>                    │
                              └───────────────────────────────────────────┘
                                            │ lỗi ở bất kỳ bước nào
                                            ▼
                              UPDATE job: status=failed, retry_count++
                              Nack message → DLQ (sau max_retries lần)
```

### 3.1. FFmpeg command (360p)

```bash
ffmpeg -i input.mp4 \
  -map 0:v:0 -map 0:a:0 \
  -c:v libx264 -crf 23 -preset fast -sc_threshold 0 \
  -g 48 -keyint_min 48 \
  -b:v 500k -maxrate 535k -bufsize 1000k \
  -vf "scale=640:-2" \
  -c:a aac -b:a 96k -ac 2 \
  -f hls \
  -hls_time 6 \
  -hls_playlist_type vod \
  -hls_segment_filename "out/360p/%04d.ts" \
  out/360p/360p.m3u8
```

Master playlist (`master.m3u8`) được **service tự generate** sau khi biết đường dẫn các rendition (không phụ thuộc FFmpeg tạo master).

---

## 4. Service Layout

```
services/transcoder/
├── cmd/
│   └── main.go                          # entry, chỉ có APP_MODE=worker
├── internal/
│   ├── config/config.go                 # Config struct (Viper / mapstructure)
│   ├── domain/job.go                    # TranscodingJob model + JobStatus constants
│   ├── dto/
│   │   ├── event.go                     # VideoUploadedEvent (consumed) + VideoTranscodedEvent (produced)
│   │   └── rendition.go                 # RenditionConfig struct
│   ├── repository/
│   │   ├── job_repository.go            # interface + GORM impl
│   │   └── mocks/                       # testify mocks
│   ├── storage/
│   │   ├── storage.go                   # interface ObjectStorage (reuse pattern từ upload-service)
│   │   └── s3.go                        # aws-sdk-go-v2 implementation (GetObject + PutObject + PresignGet)
│   ├── ffmpeg/
│   │   ├── transcoder.go                # interface Transcoder
│   │   └── ffmpeg_transcoder.go         # exec.CommandContext implementation
│   ├── worker/
│   │   ├── transcoder_worker.go         # consume loop + semaphore concurrency
│   │   └── job_processor.go             # orchestrate download → transcode → upload → publish
│   ├── publisher/
│   │   └── event_publisher.go           # publish video.transcoded / video.transcode_failed
│   └── playlist/
│       └── hls.go                       # generate master.m3u8 từ rendition list
├── migrations/
│   ├── 20260528000000_init_transcoding_jobs.sql
│   └── embed.go
├── docs/                                # swagger stub (không có HTTP API phase 1)
├── Dockerfile
├── go.mod
└── .env.example
```

---

## 5. Data Model

### 5.1. Bảng `transcoding_jobs`

```sql
-- migrations/20260528000000_init_transcoding_jobs.sql
-- +goose Up
CREATE TABLE transcoding_jobs (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    video_id          UUID NOT NULL,               -- ID từ upload-service (không FK — cross-service)
    status            VARCHAR(32) NOT NULL DEFAULT 'pending',
    retry_count       INT NOT NULL DEFAULT 0,
    max_retries       INT NOT NULL DEFAULT 3,

    -- input (từ event)
    object_key        VARCHAR(512) NOT NULL,        -- raw video key in MinIO
    bucket            VARCHAR(128) NOT NULL,
    mime_type         VARCHAR(64),
    size_bytes        BIGINT,

    -- output (điền sau khi hoàn thành)
    hls_base_path     VARCHAR(512),                 -- hls/<video_id>/
    master_playlist   VARCHAR(512),                 -- hls/<video_id>/master.m3u8
    renditions        JSONB,                        -- [{name, width, height, bitrate, playlist}]
    duration_seconds  NUMERIC(10,3),
    output_size_bytes BIGINT,

    -- tracking
    error_message     TEXT,
    started_at        TIMESTAMPTZ,
    completed_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_jobs_video_id ON transcoding_jobs(video_id);
CREATE INDEX idx_jobs_status   ON transcoding_jobs(status);
CREATE INDEX idx_jobs_pending  ON transcoding_jobs(created_at) WHERE status = 'pending';

-- +goose Down
DROP TABLE transcoding_jobs;
```

### 5.2. State machine

```
pending ──start──▶ processing ──success──▶ completed
                       │
                       └──error──▶ failed (retry_count < max_retries → re-queue)
                                         (retry_count >= max_retries → dead)
```

| Status | Mô tả |
|---|---|
| `pending` | Job được tạo khi nhận event, chưa có worker nào nhận |
| `processing` | Một worker đang chạy FFmpeg |
| `completed` | HLS upload xong, event `video.transcoded` đã phát |
| `failed` | Lỗi ở bước nào đó, còn retry |
| `dead` | Đã hết số lần retry, cần can thiệp thủ công |

### 5.3. JSONB `renditions` schema

```json
[
  { "name": "360p",  "width": 640,  "height": 360,  "bitrate_kbps": 500,  "playlist": "hls/<id>/360p/360p.m3u8" },
  { "name": "720p",  "width": 1280, "height": 720,  "bitrate_kbps": 2500, "playlist": "hls/<id>/720p/720p.m3u8" },
  { "name": "1080p", "width": 1920, "height": 1080, "bitrate_kbps": 5000, "playlist": "hls/<id>/1080p/1080p.m3u8" },
  { "name": "audio", "width": 0,    "height": 0,    "bitrate_kbps": 128,  "playlist": "hls/<id>/audio/audio.m3u8" }
]
```

---

## 6. Event Contract

### 6.1. Event consumed

| Field | Value |
|---|---|
| Exchange | `video.events` (direct, durable) |
| Queue binding | `transcoder.video.uploaded` → routing key `video.uploaded` |
| Consumer | transcoder-service worker |

Payload (từ upload-service, đọc-only):

```json
{
  "video_id":    "5b3e...",
  "object_key":  "videos/2026/05/5b3e....mp4",
  "bucket":      "videos",
  "mime_type":   "video/mp4",
  "size_bytes":  104857600,
  "etag":        "abc123",
  "owner_id":    null,
  "uploaded_at": "2026-05-28T10:00:00Z",
  "storage_url": "http://minio:9000/videos/videos/2026/05/5b3e....mp4"
}
```

### 6.2. Events published

**`video.transcoded`** — thành công:

```json
{
  "video_id":        "5b3e...",
  "job_id":          "a1b2...",
  "hls_base_path":   "hls/5b3e.../",
  "master_playlist": "hls/5b3e.../master.m3u8",
  "renditions": [
    { "name": "360p",  "playlist": "hls/5b3e.../360p/360p.m3u8",   "bitrate_kbps": 500  },
    { "name": "720p",  "playlist": "hls/5b3e.../720p/720p.m3u8",   "bitrate_kbps": 2500 },
    { "name": "1080p", "playlist": "hls/5b3e.../1080p/1080p.m3u8", "bitrate_kbps": 5000 },
    { "name": "audio", "playlist": "hls/5b3e.../audio/audio.m3u8", "bitrate_kbps": 128  }
  ],
  "duration_seconds":  327.6,
  "output_size_bytes": 524288000,
  "transcoded_at":     "2026-05-28T10:05:30Z"
}
```

**`video.transcode_failed`** — thất bại vĩnh viễn (dead):

```json
{
  "video_id":      "5b3e...",
  "job_id":        "a1b2...",
  "error_message": "ffmpeg exit code 1: codec not supported",
  "retry_count":   3,
  "failed_at":     "2026-05-28T10:06:00Z"
}
```

| Exchange | `video.events` (cùng exchange) |
| Routing keys | `video.transcoded`, `video.transcode_failed` |
| Consumers | (tương lai) video-catalog-service, search-indexer-service, thumbnail-service |

---

## 7. Rendition Configuration

Cấu hình renditions qua env var `TRANSCODER_RENDITIONS` (JSON string), mặc định hardcode:

```go
var defaultRenditions = []RenditionConfig{
    {Name: "360p",  Width: 640,  Height: 360,  VideoBitrateK: 500,  MaxRateK: 535,  BufSizeK: 1000, AudioBitrateK: 96,  AudioChannels: 2},
    {Name: "720p",  Width: 1280, Height: 720,  VideoBitrateK: 2500, MaxRateK: 2675, BufSizeK: 5000, AudioBitrateK: 128, AudioChannels: 2},
    {Name: "1080p", Width: 1920, Height: 1080, VideoBitrateK: 5000, MaxRateK: 5350, BufSizeK: 10000,AudioBitrateK: 192, AudioChannels: 2},
    {Name: "audio", Width: 0,    Height: 0,    VideoBitrateK: 0,    MaxRateK: 0,    BufSizeK: 0,    AudioBitrateK: 128, AudioChannels: 2, AudioOnly: true},
}
```

`RenditionConfig` trong `internal/dto/rendition.go`:

```go
type RenditionConfig struct {
    Name          string
    Width, Height int
    VideoBitrateK int
    MaxRateK      int
    BufSizeK      int
    AudioBitrateK int
    AudioChannels int
    AudioOnly     bool
}
```

Mỗi rendition tạo thư mục con độc lập → upload song song sau khi FFmpeg xong toàn bộ.

---

## 8. Worker & Concurrency

### 8.1. `transcoder_worker.go`

```go
type TranscoderWorker struct {
    broker     broker.Broker
    processor  *JobProcessor
    sem        chan struct{}     // semaphore = TRANSCODER_CONCURRENCY
    cfg        *config.Config
}

func (w *TranscoderWorker) Run(ctx context.Context) error {
    msgs, err := w.broker.Consume(ctx, "transcoder.video.uploaded")
    // ...
    for msg := range msgs {
        w.sem <- struct{}{}
        go func(m amqp.Delivery) {
            defer func() { <-w.sem }()
            if err := w.processor.Process(ctx, m); err != nil {
                m.Nack(false, false) // → DLQ
            } else {
                m.Ack(false)
            }
        }(msg)
    }
}
```

- **Semaphore** giới hạn số FFmpeg process chạy đồng thời.
- `m.Nack(false, false)`: không requeue vào queue chính → RabbitMQ route sang DLQ `transcoder.video.uploaded.dlq` (bind qua dead-letter-exchange `video.events.dlq`).
- Sau max_retries (lưu trong DB): không Nack mà chỉ UPDATE status=dead và publish `video.transcode_failed`.

### 8.2. `job_processor.go` — chuỗi bước

```
Process(ctx, msg)
  ├─ 1. Unmarshal VideoUploadedEvent
  ├─ 2. INSERT transcoding_jobs (status=pending)
  ├─ 3. UPDATE status=processing, started_at=NOW()
  ├─ 4. Download raw video: PresignGet → http.Get → save to /tmp/<job_id>/input.<ext>
  ├─ 5. ffmpeg.Transcoder.Transcode(ctx, inputPath, outputDir, renditions)
  │      → runs FFmpeg subprocesses (1 per rendition OR 1 combined command)
  ├─ 6. Generate master.m3u8 qua playlist.GenerateMaster(renditions, outputDir)
  ├─ 7. Upload HLS tree: storage.PutObject cho mỗi .ts + .m3u8
  │      → upload songs song: errgroup với bounded goroutines
  ├─ 8. UPDATE job: status=completed, hls_base_path, master_playlist, renditions JSON, duration, output_size
  ├─ 9. publisher.PublishVideoTranscoded(ctx, event)
  └─ 10. os.RemoveAll("/tmp/<job_id>")
```

**Error handling:**

- Bước 4-9: bất kỳ lỗi nào → `UPDATE status=failed, error_message, retry_count++`.
- Nếu `retry_count >= max_retries` → `UPDATE status=dead` + publish `video.transcode_failed` + return nil (Ack để không loop mãi trong DLQ).
- Bước 10 luôn chạy (deferred cleanup) kể cả khi lỗi để tránh disk leak.

---

## 9. FFmpeg Interface & Implementation

### 9.1. Interface

```go
// internal/ffmpeg/transcoder.go
type Transcoder interface {
    Transcode(ctx context.Context, input string, outputDir string, renditions []dto.RenditionConfig) (*TranscodeResult, error)
}

type TranscodeResult struct {
    DurationSeconds float64
    OutputSizeBytes int64
    RenditionPaths  []RenditionPath  // {Name, PlaylistPath, SegmentDir}
}
```

### 9.2. `ffmpeg_transcoder.go`

Chạy **1 FFmpeg process** với nhiều output (FFmpeg natively hỗ trợ multiple outputs trong 1 lần decode):

```go
func (t *FFmpegTranscoder) Transcode(ctx context.Context, input, outputDir string, renditions []dto.RenditionConfig) (*TranscodeResult, error) {
    args := buildFFmpegArgs(input, outputDir, renditions)
    cmd := exec.CommandContext(ctx, "ffmpeg", args...)
    cmd.Stderr = &logWriter{logger: t.logger} // stream FFmpeg stderr vào Zap
    if err := cmd.Run(); err != nil {
        return nil, fmt.Errorf("ffmpeg: %w", err)
    }
    return collectResult(outputDir, renditions)
}
```

**`buildFFmpegArgs`** tạo lệnh FFmpeg dạng:

```bash
ffmpeg -y -i input.mp4 \
  # rendition 0: 360p
  -map 0:v:0 -map 0:a:0 -c:v libx264 -vf scale=640:-2 -b:v 500k ... \
  -f hls -hls_time 6 -hls_playlist_type vod \
  -hls_segment_filename "outputDir/360p/%04d.ts" outputDir/360p/360p.m3u8 \
  # rendition 1: 720p
  -map 0:v:0 -map 0:a:0 -c:v libx264 -vf scale=1280:-2 -b:v 2500k ... \
  -f hls -hls_time 6 -hls_playlist_type vod \
  -hls_segment_filename "outputDir/720p/%04d.ts" outputDir/720p/720p.m3u8 \
  ...
```

Decode video 1 lần, encode N renditions song song → tiết kiệm đáng kể I/O và thời gian decode.

### 9.3. `collectResult`

Sau khi FFmpeg xong: `filepath.Walk(outputDir)` tính `output_size_bytes`; parse duration từ FFprobe hoặc từ playlist `.m3u8` (`#EXT-X-TARGETDURATION` + số segment).

---

## 10. HLS Master Playlist Generator

```go
// internal/playlist/hls.go
func GenerateMaster(renditions []dto.RenditionConfig, paths []RenditionPath) string
```

Output mẫu:

```m3u8
#EXTM3U
#EXT-X-VERSION:3

#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="Default",DEFAULT=YES,URI="audio/audio.m3u8"

#EXT-X-STREAM-INF:BANDWIDTH=500000,RESOLUTION=640x360,CODECS="avc1.42e01e,mp4a.40.2",AUDIO="audio"
360p/360p.m3u8

#EXT-X-STREAM-INF:BANDWIDTH=2500000,RESOLUTION=1280x720,CODECS="avc1.4d401f,mp4a.40.2",AUDIO="audio"
720p/720p.m3u8

#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080,CODECS="avc1.640028,mp4a.40.2",AUDIO="audio"
1080p/1080p.m3u8
```

Dùng **relative paths** (không absolute URL) — CDN prefix sẽ được thêm bởi player API sau này.

---

## 11. Storage Layout (MinIO / S3)

```
videos/                            ← raw uploads (upload-service)
  videos/2026/05/<video_id>.mp4

hls/                               ← HLS output (transcoder-service)
  hls/<video_id>/
    master.m3u8
    360p/
      360p.m3u8
      0000.ts
      0001.ts
      ...
    720p/
      720p.m3u8
      0000.ts
      ...
    1080p/
      1080p.m3u8
      0000.ts
      ...
    audio/
      audio.m3u8
      0000.ts
      ...
```

Bucket `videos` dùng chung với upload-service (cùng MinIO instance). Tách prefix rõ ràng để áp lifecycle policy khác nhau:
- `videos/*` → giữ nguyên raw (hoặc move to Glacier sau 30 ngày — phase 2).
- `hls/*` → public read (hoặc CDN-signed — phase 2).

---

## 12. Configuration (Env vars)

| Group | Env var | Mặc định | Ghi chú |
|---|---|---|---|
| App | `APP_MODE` | `worker` | Phase 1 chỉ có `worker` |
| App | `TIME_GRACE` | `15` | Graceful shutdown (s) |
| Database | `DATABASE_HOST/PORT/USER/PASSWORD/DBNAME/SSLMODE` | — | Postgres `transcoder_db` |
| Broker | `BROKER_HOST/PORT/USER/PASSWORD` | — | RabbitMQ |
| Broker | `BROKER_QUEUE` | `transcoder.video.uploaded` | Queue bind vào exchange `video.events` |
| Broker | `BROKER_DLQ_EXCHANGE` | `video.events.dlq` | Dead-letter exchange |
| Storage | `STORAGE_ENDPOINT` | `http://minio:9000` | |
| Storage | `STORAGE_REGION` | `us-east-1` | |
| Storage | `STORAGE_BUCKET` | `videos` | Cùng bucket với upload-service |
| Storage | `STORAGE_ACCESS_KEY/SECRET_KEY` | — | |
| Storage | `STORAGE_USE_PATH_STYLE` | `true` | |
| Transcoder | `TRANSCODER_CONCURRENCY` | `2` | Số FFmpeg jobs đồng thời |
| Transcoder | `TRANSCODER_MAX_RETRIES` | `3` | Retry trước khi dead |
| Transcoder | `TRANSCODER_TMP_DIR` | `/tmp/transcoder` | Working dir cho FFmpeg |
| Transcoder | `TRANSCODER_HLS_PREFIX` | `hls` | Prefix key trong bucket |
| Transcoder | `TRANSCODER_SEGMENT_DURATION` | `6` | HLS segment duration (giây) |
| Otel | `OTEL_ENABLED` | `false` | |
| Otel | `OTEL_EXPORTER_OTLP_ENDPOINT` | — | |

---

## 13. ★ Kỹ thuật & Package sử dụng

### 13.1. Reuse từ shared `pkg/`

| Package | Public API dùng | Mục đích |
|---|---|---|
| `pkg/config` | `Load(&cfg)` | Viper env binding |
| `pkg/db` | `New(cfg)`, `RunMigrations(...)` | GORM + goose migrations |
| `pkg/broker` | `NewRabbitMQ(cfg)`, `Broker.Consume(ctx, queue)`, `Broker.Publish(...)` | Consume `video.uploaded` + publish `video.transcoded` |
| `pkg/logger` | `Init("transcoder", env)`, `L()` | Structured Zap logging |
| `pkg/errors` | `InternalServer`, `NotFound`, ... | Error wrapping |
| `pkg/trace` | OTel init | Propagate trace qua AMQP headers |

`pkg/redis` và `pkg/ratelimit` **không dùng** — service không expose HTTP.

### 13.2. External Go packages

| Package | Mục đích |
|---|---|
| `github.com/aws/aws-sdk-go-v2/service/s3` | GetObject (download raw), PutObject (upload HLS) |
| `github.com/aws/aws-sdk-go-v2/service/s3` `NewPresignClient` | Presigned GET URL để download raw video |
| `github.com/google/uuid` | Job ID |
| `golang.org/x/sync/errgroup` | Upload HLS segments song song có bounded concurrency |
| `gorm.io/gorm` + `gorm.io/driver/postgres` | Persist transcoding_jobs |
| `github.com/pressly/goose/v3` | Migrations |
| `go.uber.org/zap` | Logger |
| `github.com/rabbitmq/amqp091-go` | (qua `pkg/broker`) |
| `github.com/stretchr/testify` | Unit test + mocks |

**Không dùng bất kỳ Go FFmpeg binding** (như `github.com/u2takey/ffmpeg-go`) — `os/exec` đơn giản hơn, dễ debug hơn, không bị phụ thuộc version FFmpeg lib.

### 13.3. Kỹ thuật & pattern áp dụng

1. **Event-driven consumer** — loose coupling hoàn toàn với upload-service; không shared DB, không HTTP call chéo.
2. **Semaphore concurrency** — giới hạn FFmpeg goroutine bằng buffered channel, tránh OOM trên máy ít RAM.
3. **Single-pass multi-output FFmpeg** — decode video 1 lần, encode N renditions, giảm ~3-4× thời gian so với N lần encode riêng.
4. **Interface-based FFmpeg** — `Transcoder` interface cho phép mock trong unit test mà không cần FFmpeg binary.
5. **Deferred cleanup** — `defer os.RemoveAll(tmpDir)` đảm bảo disk không leak dù có panic hay lỗi ở bất kỳ bước nào.
6. **Parallel upload với errgroup** — upload `.ts` segments song song, bounded bởi `TRANSCODER_UPLOAD_CONCURRENCY` (mặc định 10).
7. **Relative HLS paths** — master playlist dùng relative URL → CDN prefix thêm bởi player layer, không hardcode URL.
8. **Dead-letter queue** — message vào DLQ sau max_retries, operator xử lý thủ công; không block queue chính.
9. **Idempotent job creation** — nếu nhận cùng `video_id` 2 lần (RabbitMQ at-least-once), `INSERT ... ON CONFLICT (video_id) DO NOTHING` → skip.
10. **Graceful shutdown** — nhận SIGTERM → không nhận message mới, đợi jobs hiện tại xong (tối đa `TIME_GRACE` giây).
11. **Goose embedded migrations** — SQL đóng trong binary, chạy auto khi startup.
12. **Multi-stage Dockerfile** — build stage: `golang:1.26-alpine`; runtime stage: `alpine:latest` + `ffmpeg` package (từ Alpine apk).

---

## 14. Dockerfile

```dockerfile
# Build stage
FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY go.work go.work.sum ./
COPY pkg/ ./pkg/
COPY services/transcoder/ ./services/transcoder/
RUN go build -o /transcoder ./services/transcoder/cmd/main.go

# Runtime stage — cần FFmpeg
FROM alpine:latest
RUN apk add --no-cache ffmpeg ca-certificates tzdata
WORKDIR /app
COPY --from=builder /transcoder .
EXPOSE 8084
ENTRYPOINT ["./transcoder"]
```

FFmpeg được cài qua `apk add ffmpeg` → Alpine package (~50MB added to image). Không cần build FFmpeg from source.

---

## 15. Local Development (Docker Compose)

```yaml
postgres-transcoder:
  image: postgres:16-alpine
  environment: { POSTGRES_DB: transcoder_db, POSTGRES_USER: user_admin, POSTGRES_PASSWORD: password123 }
  ports: ["5435:5432"]
  volumes: [postgres_transcoder_data:/var/lib/postgresql/data]
  healthcheck:
    test: ["CMD-SHELL", "pg_isready -U user_admin -d transcoder_db"]

transcoder-worker:
  build:
    context: .
    dockerfile: services/transcoder/Dockerfile
  env_file: services/transcoder/.env
  environment:
    APP_MODE: worker
  depends_on:
    postgres-transcoder: { condition: service_healthy }
    rabbitmq: { condition: service_healthy }
    minio: { condition: service_healthy }
  volumes:
    - transcoder_tmp:/tmp/transcoder  # optional: mount volume cho tmp để debug

volumes:
  postgres_transcoder_data:
  transcoder_tmp:
```

**Lưu ý:** MinIO không thêm mới — dùng chung instance đã có từ upload-service.

`Makefile` thêm:

```makefile
build-transcoder:
	docker build -f services/transcoder/Dockerfile -t transcoder-service:local .

deploy-transcoder:
	docker compose up -d --build transcoder-worker
```

---

## 16. Kubernetes Layout

`k8s/services/transcoder/`:

| File | Nội dung |
|---|---|
| `01_secret_transcoder.yaml` | DB + Storage credentials |
| `02_postgres_transcoder.yaml` | PostgreSQL StatefulSet + Service |
| `03_transcoder.yaml` | ConfigMap + Deployment `transcoder-worker` |

```yaml
# 03_transcoder.yaml (Deployment excerpt)
resources:
  requests:
    cpu: "1000m"
    memory: "512Mi"
  limits:
    cpu: "4000m"      # FFmpeg CPU-intensive
    memory: "2Gi"     # buffer RAM cho video download + encode
```

**Scaling:** `replicas: 1` phase 1 (tránh cùng video được transcoded 2 lần nếu RabbitMQ at-least-once và job chưa kịp INSERT với `ON CONFLICT`). Scale ngang bằng cách tăng replica + giảm `TRANSCODER_CONCURRENCY` mỗi pod.

Không cần Ingress — service không expose HTTP ra ngoài.

---

## 17. RabbitMQ Queue Setup

Queue `transcoder.video.uploaded` cần được declare với dead-letter config:

```go
// trong worker startup:
ch.QueueDeclare("transcoder.video.uploaded", true, false, false, false, amqp.Table{
    "x-dead-letter-exchange":    "video.events.dlq",
    "x-dead-letter-routing-key": "transcoder.video.uploaded.failed",
    "x-message-ttl":             int32(86400000), // 24h trong DLQ
})
ch.QueueBind("transcoder.video.uploaded", "video.uploaded", "video.events", false, nil)

// DLQ queue
ch.QueueDeclare("transcoder.video.uploaded.dlq", true, false, false, false, nil)
ch.QueueBind("transcoder.video.uploaded.dlq", "transcoder.video.uploaded.failed", "video.events.dlq", false, nil)
```

Đặt code khai báo queue này trong `worker startup`, không phải trong `pkg/broker` (broker chỉ cung cấp primitive, topology là concern của service).

---

## 18. Verification Plan

### 18.1. Unit test

`internal/worker/job_processor_test.go` — mock `Transcoder` + `ObjectStorage` + `VideoRepository` + `Broker`:

- **Happy path**: download → transcode → upload → DB updated → event published.
- **FFmpeg failure**: transcode returns error → job status=failed, retry_count++, Nack.
- **Idempotency**: nhận video_id đã có job completed → skip (ON CONFLICT DO NOTHING).
- **Max retries**: retry_count == max_retries → status=dead + publish `video.transcode_failed`.
- **Cleanup**: `os.RemoveAll` được gọi kể cả khi lỗi.

### 18.2. End-to-end local (`make up`)

1. Upload một video thật qua upload-service (`curl` hoặc Postman).
2. Gọi `POST /api/v1/videos/<id>/complete` → event `video.uploaded` được publish.
3. Transcoder worker consume event → log thấy: `"processing job"` → `"ffmpeg started"` → `"upload hls"` → `"job completed"`.
4. MinIO Console (`http://localhost:9001`): bucket `videos/hls/<video_id>/` xuất hiện với `master.m3u8` và các thư mục rendition.
5. DB `transcoder_db`: `SELECT status, renditions, duration_seconds FROM transcoding_jobs` → `completed`.
6. RabbitMQ UI (`http://localhost:15672`): exchange `video.events` có message `video.transcoded`.

### 18.3. HLS player test

Dùng `hls.js` demo page (https://hls-js.netlify.app/demo/) với URL `http://localhost:9000/videos/hls/<video_id>/master.m3u8` → video phải phát được ở cả 3 chất lượng.

*(MinIO bucket `videos` cần set public read policy cho prefix `hls/*` — chỉ dùng cho dev local.)*

### 18.4. Failure test

1. Stop FFmpeg trong container giữa chừng: `kill -9 <ffmpeg_pid>` → job status=failed, retry_count=1.
2. Message vào DLQ sau 3 lần retry → log thấy `video.transcode_failed` event.
3. `rm` raw video khỏi MinIO trước khi worker xử lý → HeadObject fail → job=failed.

---

## 19. Roadmap mở rộng (Phase 2+)

1. **DASH output** — thêm rendition loop cho MPEG-DASH bên cạnh HLS; master `manifest.mpd`.
2. **Thumbnail extraction** — chạy `ffmpeg -ss 00:00:10 -frames:v 1 thumb.jpg` trong job_processor, hoặc tách sang `thumbnail-service` riêng consume `video.transcoded`.
3. **Progress reporting** — FFmpeg stderr parse `frame=...time=...` → update `progress_percent` trong DB → SSE endpoint cho frontend theo dõi.
4. **FFprobe pre-validation** — chạy `ffprobe -v quiet -print_format json -show_streams` trước FFmpeg để validate codec, bitrate, duration; reject video corrupt sớm.
5. **Hardware acceleration** — `libx264` → `h264_nvenc` (NVIDIA) hoặc `h264_videotoolbox` (Apple Silicon) khi chạy trên node có GPU.
6. **Exactly-once publish** — outbox table `transcoder_outbox_events` + `worker-outbox` mode để đảm bảo không mất event dù RabbitMQ down lúc publish.
7. **Multi-audio track** — map audio streams riêng, hỗ trợ đa ngôn ngữ subtitle và audio.
8. **Cost optimization** — sau transcoding xong, chuyển raw video sang S3 Glacier / MinIO tiered storage; chỉ giữ HLS hot.
