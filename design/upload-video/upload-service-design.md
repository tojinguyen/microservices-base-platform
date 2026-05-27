# Upload Service — Design Document

> Service mới: `upload-service` — đóng vai trò ingestion layer cho một nền tảng video kiểu YouTube. Tài liệu này mô tả thiết kế, contract API, data model, infrastructure, và **toàn bộ kỹ thuật / package** được sử dụng để tích hợp vào platform hiện có (`identity` + `notification` + shared `pkg/`).

---

## 1. Context & Mục tiêu

Platform hiện có 2 service Go: `identity` (auth/JWT/OAuth) và `notification` (email/webhook qua RabbitMQ), cùng module dùng chung `pkg/`. Cần thêm `upload-service` chịu trách nhiệm:

- Nhận yêu cầu tạo video mới + cấp **presigned URL** cho client upload trực tiếp lên object storage.
- Quản lý metadata video (title, description, mime, size, etag, status…).
- Phát event `video.uploaded` qua RabbitMQ để các service downstream (transcoder, thumbnail, indexer…) consume sau này.

**Out of scope** (phase 1):

- Transcoding (HLS/DASH multi-bitrate) — service riêng.
- Thumbnail / preview frame generation — service riêng.
- Authentication / ownership enforcement — tạm public (giống `notification-service` hiện tại), thêm sau bằng `pkg/auth`.
- CDN signed download, resumable / TUS, multipart presigned cho file > 5GB — phase 2.

---

## 2. Quyết định thiết kế cốt lõi

| Quyết định | Lựa chọn | Lý do |
|---|---|---|
| Upload flow | **Presigned PUT URL** — client upload trực tiếp lên storage, service không proxy bytes | Stateless, scale ngang dễ, không tốn RAM/CPU/bandwidth của API. Pattern chuẩn của YouTube/Vimeo. |
| Confirm mechanism | **Explicit `POST /complete` từ client + janitor worker** (không dựa vào S3 bucket notification) | Service độc lập với cấu hình bucket; dev local chạy MinIO không cần webhook. Janitor bù cho trường hợp client mất kết nối. |
| Storage backend | **S3-compatible abstraction** dùng `aws-sdk-go-v2` với `BaseEndpoint` override | Một codepath cho cả MinIO (dev/local/K8s) và AWS S3 thật (prod). Đổi backend chỉ qua env. |
| Worker pattern | **Cùng binary, `APP_MODE` env** điều phối (`api` / `worker-janitor`) | Bám sát pattern `notification-service` (api / worker-email / worker-webhook / worker-outbox / worker-dlq / worker-scheduler / worker-campaign). |
| Event contract | Publish lên exchange mới `video.events`, routing key `video.uploaded` | Service downstream sau này tự subscribe. Service này không consume gì. |
| Auth | Không có middleware auth ở phase 1, chỉ có ratelimit | Đồng bộ trạng thái hiện tại của `notification-service`. |

---

## 3. Kiến trúc & Vòng đời upload

```
┌────────┐  1. POST /api/v1/videos            ┌──────────────────┐
│ Client │ ────────────────────────────────▶ │  upload-service  │
│        │ ◀────────────── presigned PUT URL  │     (API mode)    │
└───┬────┘                                    └─────────┬────────┘
    │                                                   │ INSERT videos(status=pending_upload)
    │  2. PUT <presigned_url> (raw bytes)               │
    │ ─────────────────────────────────────────────────▶│
    │                  ┌──────────────────┐             │
    │                  │  S3 / MinIO      │ ◀───────────┘ (key, bucket, TTL=15m)
    │                  └──────────────────┘
    │
    │  3. POST /api/v1/videos/{id}/complete
    │ ─────────────────────────────────────────────────▶ upload-service
    │                                                   │  HeadObject → verify
    │                                                   │  UPDATE status=uploaded, size, etag
    │                                                   │  Publish "video.uploaded" → RabbitMQ
    │ ◀──────────── 200 OK { status: uploaded } ────────│
    │
    │  (Optional) POST /api/v1/videos/{id}/abort
    │ ─────────────────────────────────────────────────▶ upload-service (DeleteObject + status=aborted)


Background (APP_MODE=worker-janitor, mỗi 5 phút):
  SELECT * FROM videos WHERE status='pending_upload' AND upload_expires_at < NOW()
  → HeadObject(key): nếu tồn tại → status=uploaded + publish; nếu không → status=expired
```

---

## 4. Service Layout (theo Standard Go Project Layout)

```
services/upload/
├── cmd/
│   └── main.go                       # entry, APP_MODE switch
├── internal/
│   ├── config/config.go              # Config struct + Load qua pkg/config (Viper)
│   ├── domain/video.go               # Video model + VideoStatus constants
│   ├── dto/
│   │   ├── api.go                    # request/response DTOs
│   │   └── event.go                  # event payloads (video.uploaded)
│   ├── handler/handler.go            # VideoHandler (Init, Complete, Abort, Get, List)
│   ├── service/video_service.go      # business logic (orchestrate storage + repo + broker)
│   ├── repository/
│   │   ├── video_repository.go       # interface + GORM impl
│   │   └── mocks/                    # testify mocks
│   ├── storage/                      # ★ MỚI — S3-compatible client
│   │   ├── storage.go                # interface ObjectStorage
│   │   └── s3.go                     # aws-sdk-go-v2 implementation
│   ├── worker/janitor_worker.go      # APP_MODE=worker-janitor
│   ├── route/route.go                # Gin route registration
│   └── publisher/event_publisher.go  # wrapper trên pkg/broker để publish video events
├── migrations/
│   ├── 20260527000000_init_videos_table.sql
│   └── migrations.go                 # embed.FS
├── docs/                             # swagger generated
├── Dockerfile
├── go.mod
└── .env.example
```

`go.work` ở root sẽ thêm `./services/upload`.

---

## 5. Data Model

### 5.1. Bảng `videos`

```sql
-- migrations/20260527000000_init_videos_table.sql
-- +goose Up
CREATE TABLE videos (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title             VARCHAR(255) NOT NULL,
    description       TEXT,
    owner_id          UUID,                                 -- để trống ở phase 1
    object_key        VARCHAR(512) NOT NULL UNIQUE,         -- videos/2026/05/<uuid>.mp4
    bucket            VARCHAR(128) NOT NULL,
    mime_type         VARCHAR(64),
    size_bytes        BIGINT,
    etag              VARCHAR(128),
    status            VARCHAR(32) NOT NULL DEFAULT 'pending_upload',
    storage_url       VARCHAR(1024),                        -- canonical (không phải presigned)
    upload_expires_at TIMESTAMPTZ,
    uploaded_at       TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_videos_status ON videos(status);
CREATE INDEX idx_videos_owner ON videos(owner_id);
CREATE INDEX idx_videos_pending_expiry
    ON videos(upload_expires_at)
    WHERE status = 'pending_upload';

-- +goose Down
DROP TABLE videos;
```

### 5.2. State machine

```
pending_upload ──complete──▶ uploaded ──(downstream service)──▶ transcoding / ready / failed
       │
       ├──abort──▶ aborted
       └──janitor (expired & not in storage)──▶ expired
```

Service này chỉ chịu trách nhiệm tới `uploaded`. Các trạng thái sau là contract cho service tương lai consume.

---

## 6. API Surface

Tất cả qua Gin, prefix `/api/v1`. Response envelope dùng `pkg/response.StandardResponse` (`{success, data, meta, error}`).

| # | Method | Path | Mục đích |
|---|---|---|---|
| 1 | POST | `/api/v1/videos` | Init upload — tạo metadata + presigned PUT URL |
| 2 | POST | `/api/v1/videos/:id/complete` | Confirm upload xong → HeadObject + publish event |
| 3 | POST | `/api/v1/videos/:id/abort` | Hủy → DeleteObject + status=aborted |
| 4 | GET  | `/api/v1/videos/:id` | Lấy metadata |
| 5 | GET  | `/api/v1/videos` | List (paginate `?page=&page_size=`, lọc `?status=`) |

Tất cả route đi qua middleware **RateLimit** (Redis-backed, fixed-window, qua `pkg/ratelimit`).

### 6.1. Init upload

```http
POST /api/v1/videos
Content-Type: application/json

{
  "title": "My first video",
  "description": "Hello world",
  "mime_type": "video/mp4",
  "size_bytes": 1048576
}
```

Validation:
- `mime_type` ∈ allowlist (`video/mp4`, `video/quicktime`, `video/webm`) — cấu hình qua env.
- `size_bytes` ≤ `cfg.Upload.MaxSizeBytes` (mặc định 5 GiB).
- `title` không rỗng, ≤ 255 ký tự.

Response:

```json
{
  "success": true,
  "data": {
    "video_id": "5b3e...",
    "object_key": "videos/2026/05/5b3e....mp4",
    "bucket": "videos",
    "upload_url": "https://minio.localhost/videos/...?X-Amz-Signature=...",
    "upload_method": "PUT",
    "expires_at": "2026-05-27T10:15:00Z",
    "status": "pending_upload"
  }
}
```

### 6.2. Complete

```http
POST /api/v1/videos/{id}/complete
```

Idempotent — gọi lại trên row đã `uploaded` trả về 200 với state hiện tại (không HeadObject lại).

Behavior:
1. `HeadObject(object_key)`. Nếu không tồn tại → `400 BadRequest("object not found in storage")`.
2. UPDATE `videos` SET `status='uploaded'`, `size_bytes`, `etag`, `uploaded_at=NOW()`.
3. Publish event `video.uploaded` qua `pkg/broker.Publish`.

### 6.3. Abort

`DeleteObject` (no-op nếu không tồn tại) → status=`aborted`. Idempotent.

---

## 7. Event Contract

| Field | Value |
|---|---|
| Exchange | `video.events` (direct, durable) |
| Routing key | `video.uploaded` |
| Body (JSON) | `{ video_id, object_key, bucket, mime_type, size_bytes, etag, owner_id, uploaded_at, storage_url }` |
| Producer | upload-service (API + worker-janitor) |
| Consumers | (tương lai) transcoder-service, thumbnail-service, search-indexer-service |

Service không subscribe queue nào ở phase 1.

---

## 8. Storage Abstraction

### 8.1. Interface

```go
// internal/storage/storage.go
type ObjectStorage interface {
    PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (
        url string, expiresAt time.Time, err error)

    HeadObject(ctx context.Context, key string) (
        sizeBytes int64, etag string, exists bool, err error)

    DeleteObject(ctx context.Context, key string) error

    PublicURL(key string) string  // canonical, non-presigned
}
```

### 8.2. Implementation S3-compatible

`internal/storage/s3.go` dùng `aws-sdk-go-v2`:

- `aws/config` để load static credentials từ env.
- `s3.NewFromConfig(cfg, func(o *s3.Options) { o.BaseEndpoint = ...; o.UsePathStyle = true })` — `BaseEndpoint` rỗng → AWS S3 thật; có giá trị (vd `http://minio:9000`) → MinIO. `UsePathStyle=true` bắt buộc cho MinIO.
- `s3.NewPresignClient(client).PresignPutObject(...)` cho presigned URL.
- `client.HeadObject(...)`, `client.DeleteObject(...)` cho confirm / abort.

### 8.3. Key scheme

`videos/{YYYY}/{MM}/{video_id}{ext}` — partition theo tháng, dễ áp lifecycle policy về sau (move to glacier sau N ngày, v.v.).

---

## 9. Worker Mode

### 9.1. `APP_MODE=worker-janitor`

Loop mỗi `cfg.Janitor.IntervalSeconds` (mặc định 300s):

1. `SELECT id, object_key FROM videos WHERE status='pending_upload' AND upload_expires_at < NOW() LIMIT cfg.Janitor.BatchSize` (mặc định 100).
2. Với mỗi row: `HeadObject(key)`.
   - **Tồn tại** → UPDATE status=uploaded + size + etag + publish event (recover trường hợp client mất kết nối ngay khi gọi /complete).
   - **Không tồn tại** → UPDATE status=expired.
3. Hết batch → sleep → lặp.

Triển khai như Deployment riêng `upload-worker-janitor` (1 replica).

---

## 10. Configuration (Env vars)

Loaded qua `pkg/config.Load(&cfg)` (Viper, struct tag `mapstructure`).

| Group | Env var | Mặc định | Ghi chú |
|---|---|---|---|
| App | `APP_MODE` | `api` | `api` \| `worker-janitor` |
| App | `TIME_GRACE` | `15` | Graceful shutdown (s) |
| App | `SERVER_PORT` | `8083` | |
| Database | `DATABASE_HOST/PORT/USER/PASSWORD/NAME/SSLMODE` | — | Postgres `upload_db` |
| Redis | `REDIS_HOST/PORT/DB` | — | Cho ratelimit |
| Broker | `BROKER_HOST/PORT/USER/PASSWORD` | — | RabbitMQ |
| Broker | `QUEUE_EXCHANGE` | `video.events` | |
| Storage | `STORAGE_ENDPOINT` | `http://minio:9000` | Rỗng → AWS S3 thật |
| Storage | `STORAGE_REGION` | `us-east-1` | |
| Storage | `STORAGE_BUCKET` | `videos` | |
| Storage | `STORAGE_ACCESS_KEY` / `STORAGE_SECRET_KEY` | — | |
| Storage | `STORAGE_USE_PATH_STYLE` | `true` | Bắt buộc với MinIO |
| Upload | `UPLOAD_MAX_SIZE_BYTES` | `5368709120` (5 GiB) | |
| Upload | `UPLOAD_PRESIGN_TTL_SECONDS` | `900` (15 min) | |
| Upload | `UPLOAD_ALLOWED_MIME_TYPES` | `video/mp4,video/quicktime,video/webm` | CSV |
| Janitor | `JANITOR_INTERVAL_SECONDS` | `300` | |
| Janitor | `JANITOR_BATCH_SIZE` | `100` | |
| Ratelimit | `RATELIMIT_LIMIT` / `RATELIMIT_WINDOW_SECONDS` | `60` / `60` | |
| Otel | `OTEL_ENABLED` / `OTEL_EXPORTER_OTLP_ENDPOINT` | `false` / — | |

---

## 11. ★ Kỹ thuật & Package sử dụng

### 11.1. Reuse từ shared `pkg/` (không viết lại)

| Package | Public API dùng | Mục đích trong upload-service |
|---|---|---|
| `pkg/config` | `Load(&cfg)` | Đọc env qua Viper, bind `mapstructure` tag |
| `pkg/db` | `New(cfg)`, `RunMigrations(sqlDB, migrations.FS, ".")`, `WithTx(db, fn)` | Khởi tạo GORM/Postgres pool (25 open / 10 idle / 1h lifetime); chạy goose migrations từ `embed.FS`; transaction helper |
| `pkg/broker` | `NewRabbitMQ(cfg)`, `Broker.Publish(ctx, exchange, key, body)` | Publish event `video.uploaded` lên `video.events`. Auto-reconnect logic có sẵn |
| `pkg/redis` | `New(cfg)` | Redis client (15 conn pool) cho ratelimit |
| `pkg/ratelimit` | `New(client, Config)` + `Allow(ctx, key)` | Fixed-window rate limit (Lua + INCR) áp vào toàn bộ route |
| `pkg/logger` | `Init("upload", env)`, `L() *zap.Logger` | Structured logging JSON (Zap), trường `service`/`env` cho Loki |
| `pkg/response` | `OK`, `Created`, `OKWithMeta`, `Error` | Envelope `{success, data, meta, error}` |
| `pkg/errors` | `BadRequest`, `NotFound`, `InternalServer`, `TooManyRequests` | AppError integrate với `response.Error` |
| `pkg/trace` | OTel init (nếu `OTEL_ENABLED=true`) | Propagate trace qua HTTP + AMQP headers |
| (chưa dùng) `pkg/auth` | — | Reserved cho phase 2 khi bật ownership |

### 11.2. External Go packages (third-party)

| Package | Version target | Mục đích |
|---|---|---|
| `github.com/gin-gonic/gin` | v1.x | HTTP framework (đồng bộ với identity/notification) |
| `gorm.io/gorm` + `gorm.io/driver/postgres` | latest | ORM cho Postgres |
| `github.com/pressly/goose/v3` | v3 | DB migrations (chạy qua `pkg/db.RunMigrations`) |
| `github.com/google/uuid` | latest | Sinh UUID cho `videos.id` & object key |
| `github.com/rabbitmq/amqp091-go` | (đã có ở `pkg/broker`) | AMQP transport |
| `github.com/redis/go-redis/v9` | (đã có ở `pkg/redis`) | Redis client |
| `go.uber.org/zap` | (đã có ở `pkg/logger`) | Structured logger |
| **`github.com/aws/aws-sdk-go-v2`** | latest | ★ Core SDK |
| **`github.com/aws/aws-sdk-go-v2/config`** | latest | ★ Load AWS config |
| **`github.com/aws/aws-sdk-go-v2/credentials`** | latest | ★ Static credentials provider |
| **`github.com/aws/aws-sdk-go-v2/service/s3`** | latest | ★ S3 client (HeadObject, DeleteObject) |
| **`github.com/aws/aws-sdk-go-v2/service/s3.NewPresignClient`** | latest | ★ Presigned URL generator |
| `github.com/spf13/viper` | (đã có ở `pkg/config`) | Env binding |
| `github.com/swaggo/swag` + `swaggo/gin-swagger` | latest | Sinh + serve Swagger docs |
| `github.com/stretchr/testify` | latest | Unit test assertions + mocks |
| `go.opentelemetry.io/otel` + exporters | latest | Tracing (nếu bật) |

### 11.3. Kỹ thuật & pattern áp dụng

1. **Presigned URL upload** — offload bandwidth khỏi service, tăng throughput, giảm chi phí compute.
2. **Standard Go Project Layout** — `cmd/`, `internal/`, `pkg/`.
3. **Hexagonal-ish layering** — Handler → Service → Repository, với interface ở repo + storage để dễ mock.
4. **APP_MODE single-binary multi-role** — cùng image, env switch giữa API và worker (đồng bộ với notification-service).
5. **Idempotent endpoints** — `/complete` và `/abort` an toàn khi client retry.
6. **Janitor pattern** — bù trừ cho lost-confirm + thu dọn `pending_upload` quá hạn.
7. **S3-compatible abstraction** — một interface `ObjectStorage`, đổi backend bằng env.
8. **Outbox-free event publishing (phase 1)** — publish trực tiếp trong handler `/complete`. Nếu mất message, janitor sẽ phát hiện và publish lại (vì state vẫn `pending_upload` cho tới khi UPDATE thành công). Phase 2 có thể đưa về outbox pattern giống `notification.outbox_events` nếu cần exactly-once strict.
9. **Goose embedded migrations** — `embed.FS` đóng gói SQL vào binary, chạy tự động khi startup.
10. **Multi-stage Docker build** — `golang:1.26-alpine` (build + `swag init`) → `alpine:latest` (runtime), CGO disabled, binary tĩnh.
11. **Graceful shutdown** — context cancel khi nhận SIGTERM/SIGINT, đợi tối đa `TIME_GRACE` giây.
12. **Structured logging + tracing** — Zap JSON + OTel headers propagate qua AMQP, dễ tích hợp Loki + Grafana sẵn có.
13. **Rate limiting Redis-backed** — đồng bộ với notification-service.
14. **Repository mocks bằng testify** — unit-test service layer độc lập với DB/storage thật.

---

## 12. Local Development (Docker Compose)

`docker-compose.yml` (root) thêm các block:

```yaml
postgres-upload:
  image: postgres:16-alpine
  environment: { POSTGRES_DB: upload_db, POSTGRES_USER: user_admin, POSTGRES_PASSWORD: password123 }
  ports: ["5434:5432"]
  volumes: [pg_upload_data:/var/lib/postgresql/data]

minio:
  image: minio/minio
  command: server /data --console-address ":9001"
  environment: { MINIO_ROOT_USER: minioadmin, MINIO_ROOT_PASSWORD: minioadmin }
  ports: ["9000:9000", "9001:9001"]
  volumes: [minio_data:/data]

minio-init:
  image: minio/mc
  depends_on: [minio]
  entrypoint: >
    /bin/sh -c "
    mc alias set local http://minio:9000 minioadmin minioadmin &&
    mc mb --ignore-existing local/videos
    "

upload-service:
  build: { context: ., dockerfile: services/upload/Dockerfile }
  env_file: services/upload/.env
  depends_on: [postgres-upload, rabbitmq, minio, minio-init]
  ports: ["8083:8083"]

upload-worker-janitor:
  build: { context: ., dockerfile: services/upload/Dockerfile }
  env_file: services/upload/.env
  environment: { APP_MODE: worker-janitor }
  depends_on: [postgres-upload, rabbitmq, minio]
```

`Makefile` thêm targets: `build-upload`, `deploy-upload`, `migrate-add SERVICE=upload NAME=...`.

---

## 13. Kubernetes Layout

`k8s/services/upload/`:

| File | Nội dung |
|---|---|
| `05_secret_upload.yaml` | DB + Storage credentials |
| `06_postgres_upload.yaml` | Postgres StatefulSet + Service |
| `07_minio.yaml` | MinIO StatefulSet + Service + Console ingress `minio.localhost` |
| `09_upload.yaml` | ConfigMap + Service + 2 Deployments: `upload-api` (2 replicas) + `upload-worker-janitor` (1 replica) |

Ingress (`k8s/core/ingress.yaml`) thêm:

```
/api/v1/videos/*       → upload-service:8083
/upload/swagger/*      → upload-service:8083
```

Hosts file (Windows): thêm `127.0.0.1 minio.localhost` để truy cập MinIO Console.

---

## 14. Verification Plan

### 14.1. Unit test

- `internal/service/video_service_test.go` — mock `ObjectStorage` + `VideoRepository` + `Broker`, test các nhánh:
  - Init: validate mime/size, sinh key đúng pattern, trả URL đúng.
  - Complete: HeadObject fail → BadRequest; success → state=uploaded + publish event đúng exchange/key.
  - Complete idempotent: state đã uploaded → trả 200 không HeadObject lại.
  - Abort: DeleteObject được gọi, state=aborted.

### 14.2. End-to-end local (`make up`)

1. `curl -X POST http://localhost:8083/api/v1/videos -d '{"title":"t","mime_type":"video/mp4","size_bytes":1024}'` → nhận `upload_url`.
2. `curl -X PUT --data-binary @sample.mp4 <upload_url>` → 200 từ MinIO.
3. `curl -X POST http://localhost:8083/api/v1/videos/<id>/complete` → `{status: uploaded, size_bytes, etag}`.
4. RabbitMQ management UI (`http://localhost:15672`): exchange `video.events` có message khớp.
5. MinIO Console (`http://localhost:9001`): object hiện diện trong bucket `videos`.

### 14.3. Janitor test

1. POST init, không upload.
2. `psql -d upload_db -c "UPDATE videos SET upload_expires_at = NOW() - INTERVAL '1 hour' WHERE id='...'"`.
3. Chạy 1 process `APP_MODE=worker-janitor`, log thấy 1 row → status=`expired`.

### 14.4. K8s smoke

`make deploy-upload` → `kubectl get pods -n <ns>` thấy `upload-api` (2 ready) + `upload-worker-janitor` (1 ready). `curl localhost/api/v1/videos -X POST ...` qua ingress hoạt động.

---

## 15. Roadmap mở rộng (Phase 2+)

1. **Auth & ownership** — bật `pkg/auth.GinRequireAuth()`, gán `owner_id` từ JWT claims, list endpoint lọc theo owner.
2. **Multipart presigned** — cho file > 5 GiB hoặc mạng kém: `CreateMultipartUpload` → presigned `UploadPart` x N → `CompleteMultipartUpload`.
3. **Outbox pattern** — bảng `upload_outbox_events` + `worker-outbox` để đảm bảo exactly-once publish khi RabbitMQ down lúc gọi `/complete`.
4. **CDN signed download** — endpoint `GET /videos/:id/playback-url` sinh presigned GET URL hoặc CloudFront signed URL.
5. **Transcoding service** mới (separate repo/service), subscribe `video.uploaded`, ghi state ngược qua API hoặc qua event `video.transcoded`.
6. **Soft delete + retention policy** — lifecycle rule trên bucket + cron archive.
