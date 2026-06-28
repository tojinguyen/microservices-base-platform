# Upload Service — Flow & Logic Documentation

## 1. Tổng quan

Upload service giải quyết bài toán: **upload video file lớn lên object storage (S3/MinIO) một cách đáng tin cậy**, với hai chiến lược tuỳ theo kích thước file.

| Chiến lược | Khi nào dùng | Cơ chế |
|------------|-------------|--------|
| **Simple Upload** | File nhỏ | Client nhận 1 presigned PUT URL → upload trực tiếp lên S3 |
| **Multipart Upload** | File lớn (chia thành nhiều chunk) | Client nhận N presigned URL (1 per part) → upload song song từng part → server ghép |

Cả hai đều dùng **presigned URL**: server không làm proxy truyền bytes, client upload thẳng lên S3. Server chỉ điều phối metadata và trạng thái.

---

## 2. Runtime Modes

Binary duy nhất chạy ở 2 chế độ qua biến môi trường `APP_MODE`:

```
APP_MODE=api             → HTTP server (port 8083)
APP_MODE=worker-janitor  → Background cleanup worker (không có HTTP)
```

---

## 3. Simple Upload Flow

### Tổng quan

```
Client                    Upload Service               S3
  │                            │                        │
  │── POST /videos ──────────► │                        │
  │                            │── PresignPut() ───────►│
  │                            │◄── presigned URL ──────│
  │◄── videoID + uploadURL ────│                        │
  │                            │                        │
  │── PUT {uploadURL} ─────────────────────────────────►│
  │◄── 200 OK ─────────────────────────────────────────│
  │                            │                        │
  │── POST /videos/:id/complete►│                        │
  │                            │── HeadObject() ───────►│
  │                            │◄── size + etag ────────│
  │                            │── Publish event        │
  │◄── 200 OK ─────────────────│                        │
```

### Bước 1 — Init Upload (`POST /api/v1/videos`)

**Request:**
```json
{ "title": "My Video", "mime_type": "video/mp4", "size_bytes": 52428800 }
```

**Service logic (`VideoService.InitUpload`):**

1. Validate `mime_type` có trong allow-list (`UPLOAD_ALLOWED_MIME_TYPES`).
2. Validate `size_bytes` ≤ `UPLOAD_MAX_SIZE_BYTES`.
3. Generate `videoID` (UUID).
4. Build `object_key`: `videos/{year}/{month:02d}/{videoID}.mp4`
5. Gọi `storage.PresignPut(ctx, bucket, objectKey, ttl)` → nhận presigned PUT URL từ S3.
6. Build `storage_url` = `{PUBLIC_URL_BASE}/{objectKey}` (URL public để đọc sau).
7. Lưu `Video` record vào DB:
   - `status = pending_upload`
   - `upload_expires_at = now + presign_ttl`
8. Trả về: `videoID`, `object_key`, `bucket`, `upload_url`, `expires_at`.

**Presigned PUT URL là gì?**
S3 tạo một URL tạm thời có chữ ký (HMAC-SHA256 embedded). Client dùng URL này để PUT file trực tiếp lên S3 mà không cần AWS credentials. URL có TTL, hết hạn thì không dùng được nữa.

### Bước 2 — Client upload

Client tự PUT file binary lên `upload_url`:
```
PUT {presigned_url}
Content-Type: video/mp4
Content-Length: 52428800

[raw bytes]
```
S3 xác thực chữ ký, lưu object. Server không nhận byte nào.

### Bước 3 — Complete Upload (`POST /api/v1/videos/:id/complete`)

**Service logic (`VideoService.CompleteUpload`):**

1. Lấy Video record từ DB.
2. Gọi `storage.HeadObject(ctx, bucket, objectKey)` → kiểm tra object có tồn tại không, lấy `size` và `etag`.
   - HEAD request không download file, chỉ lấy metadata.
   - Nếu object không tồn tại → trả lỗi (client chưa upload).
3. Cập nhật Video:
   - `status = uploaded`
   - `size_bytes`, `etag` (từ S3 response)
   - `uploaded_at = now`
4. Publish event `video.uploaded` lên RabbitMQ exchange `video.events`.

### Abort (`POST /api/v1/videos/:id/abort`)

1. Gọi `storage.DeleteObject()` → xoá object trên S3 (nếu đã upload).
2. Cập nhật `status = aborted`.

---

## 4. Multipart Upload Flow

Dùng cho file lớn. S3 Multipart Upload cho phép chia file thành nhiều part (tối thiểu 5MB/part), upload song song, sau đó ghép lại.

### Tổng quan

```
Client                    Upload Service               S3
  │                            │                        │
  │── POST /uploads/sessions──►│                        │
  │                            │── CreateMultipartUpload►│
  │                            │◄── s3UploadID ─────────│
  │◄── sessionID + parts[] ────│                        │
  │                            │                        │
  │  [Với mỗi part i:]         │                        │
  │── POST /sessions/:id/parts/:i/url ─────────────────►│
  │◄── presigned part URL ─────│                        │
  │── PUT {partURL} ───────────────────────────────────►│
  │◄── ETag header ────────────────────────────────────│
  │── POST /sessions/:id/parts/:i/confirm ─────────────►│  (gửi kèm etag)
  │◄── uploaded count / total ─│                        │
  │                            │                        │
  │── POST /sessions/:id/complete ─────────────────────►│
  │                            │── CompleteMultipart() ►│
  │                            │◄── final ETag ─────────│
  │◄── 200 OK ─────────────────│                        │
```

### Bước 1 — Init Chunked Upload (`POST /api/v1/uploads/sessions`)

**Request:**
```json
{
  "video_id": "uuid",
  "mime_type": "video/mp4",
  "total_size_bytes": 524288000,
  "part_size_bytes": 10485760
}
```

**Service logic (`UploadSessionService.InitChunkedUpload`):**

1. Validate mime type + size.
2. Tính `totalParts = ceil(totalSizeBytes / partSizeBytes)`.
   - Part size tối thiểu 5MB (S3 giới hạn).
   - Tối đa 10,000 parts (S3 giới hạn).
3. Kiểm tra `Video` tồn tại và `status = pending_upload`.
4. Gọi `storage.CreateMultipartUpload(ctx, bucket, objectKey, mimeType)` → nhận `s3UploadID`.

   **`s3UploadID` là gì?**
   S3 tạo một phiên upload và trả về một ID định danh phiên đó. Mọi thao tác tiếp theo (upload part, complete, abort) đều phải gửi kèm ID này. ID này được lưu vào `UploadSession.S3UploadID`.

5. Gọi `repo.CreateSession()` — **atomic transaction**:
   - Insert 1 `UploadSession` record.
   - Insert N `UploadPart` records (1 per part), tất cả `status = pending`.
6. Nếu bất kỳ bước DB nào fail → gọi `storage.AbortMultipartUpload()` để dọn S3 trước khi trả lỗi.

**Response:**
```json
{
  "session_id": "uuid",
  "total_parts": 50,
  "part_size_bytes": 10485760,
  "expires_at": "2026-06-29T10:00:00Z"
}
```

### Bước 2 — Lấy presigned URL cho từng part (`POST /sessions/:id/parts/:number/url`)

**Service logic (`UploadSessionService.GetPartURL`):**

1. Validate session status phải là `initiated` hoặc `in_progress`.
2. Validate part number hợp lệ (1 ≤ n ≤ totalParts).
3. Gọi `storage.GeneratePresignedPartURL(ctx, bucket, objectKey, s3UploadID, partNumber, ttl)`.

   S3 tạo presigned URL riêng cho từng part, embedded thêm `partNumber` và `uploadId` vào query string.

4. Lưu `presign_url` và `presign_expires_at` vào `UploadPart` record.
5. **Auto-transition**: Nếu session đang `initiated` → chuyển sang `in_progress` (lần đầu có part được request URL).

**Tại sao không tạo sẵn URL cho tất cả parts ngay từ đầu?**
Presigned URL có TTL ngắn. Nếu tạo sẵn 1000 URL, URL đầu tiên hết hạn trước khi client dùng tới. Client nên xin URL từng cái ngay trước khi upload.

### Bước 3 — Client upload part

```
PUT {presigned_part_url}
Content-Length: 10485760

[chunk bytes]
```

S3 trả về header `ETag: "abc123..."` trong response. Client phải lưu ETag này lại.

### Bước 4 — Confirm part (`POST /sessions/:id/parts/:number/confirm`)

**Request:**
```json
{ "etag": "abc123...", "size_bytes": 10485760 }
```

**Service logic (`UploadSessionService.ConfirmPart`):**

1. Validate session status.
2. Lưu vào `UploadPart`:
   - `etag` (strip quotes nếu có — S3 đôi khi trả `"abc123"` với dấu nháy).
   - `size_bytes`, `status = uploaded`, `uploaded_at = now`.
3. Đếm số parts đã uploaded → trả về `{ uploaded: X, total: N }`.

**Tại sao phải lưu ETag?**
Khi gọi `CompleteMultipartUpload`, S3 yêu cầu danh sách `[{partNumber, etag}]` để xác thực và ghép file theo đúng thứ tự.

### Bước 5 — Complete Session (`POST /sessions/:id/complete`)

**Service logic (`UploadSessionService.CompleteSession`):**

1. Lấy session + tất cả parts.
2. Kiểm tra tất cả parts đều `status = uploaded`.
3. **Optimistic locking** — `TransitionSessionStatus(id, in_progress → completing)`:
   ```sql
   UPDATE upload_sessions
   SET status = 'completing'
   WHERE id = ? AND status = 'in_progress'
   ```
   Trả về `rowsAffected`. Nếu = 0 → có goroutine khác đang complete → trả lỗi conflict.
   
   Mục đích: tránh gọi `CompleteMultipartUpload` hai lần (S3 chỉ cho gọi một lần).

4. Build danh sách parts: `[{PartNumber: 1, ETag: "..."}, {PartNumber: 2, ETag: "..."}, ...]`
5. Gọi `storage.CompleteMultipartUpload(ctx, bucket, objectKey, s3UploadID, parts)`.
   
   S3 ghép tất cả parts theo `PartNumber`, kiểm tra ETag từng part, trả về ETag của file hoàn chỉnh.

6. Cập nhật `Video`: `status = uploaded`, `etag`, `size_bytes`, `uploaded_at`.
7. Cập nhật `UploadSession`: `status = completed`, `completed_at = now`.
8. Publish event `video.uploaded`.

### Abort Session (`POST /sessions/:id/abort`)

1. Gọi `storage.AbortMultipartUpload(ctx, bucket, objectKey, s3UploadID)`.
   
   S3 xoá tất cả parts đã upload trong phiên → giải phóng storage.

2. Cập nhật `status = aborted`.

---

## 5. Storage Interface

```go
type ObjectStorage interface {
    // Simple upload
    PresignPut(ctx, bucket, key, ttl)           → (url, error)
    HeadObject(ctx, bucket, key)                → (size, etag, exists, error)
    DeleteObject(ctx, bucket, key)              → error
    PublicURL(key)                              → string

    // Multipart upload
    CreateMultipartUpload(ctx, bucket, key, mime) → (s3UploadID, error)
    GeneratePresignedPartURL(ctx, bucket, key, uploadID, partNum, ttl) → (url, error)
    CompleteMultipartUpload(ctx, bucket, key, uploadID, parts) → (etag, error)
    AbortMultipartUpload(ctx, bucket, key, uploadID) → error
    ListMultipartParts(ctx, bucket, key, uploadID) → ([]PartInfo, error)
}
```

Implementation (`S3Storage`) dùng AWS SDK v2, hỗ trợ MinIO qua custom endpoint + path-style URL (`s3.example.com/bucket/key` thay vì `bucket.s3.amazonaws.com/key`).

**ETag normalization:**
```go
// S3 trả về etag có thể có hoặc không có dấu nháy: "abc123" hoặc abc123
etag = strings.Trim(etag, `"`)
```

---

## 6. Janitor Worker

Chạy ở chế độ `APP_MODE=worker-janitor`. Hai goroutine độc lập chạy theo interval.

### 6.1 Dọn Video hết hạn (`runBatch`)

```
Mỗi {JANITOR_INTERVAL_SECONDS} giây:

SELECT * FROM videos
WHERE status = 'pending_upload' AND upload_expires_at < NOW()
LIMIT {BATCH_SIZE}

Với mỗi video:
  → HeadObject(bucket, object_key)
  
  Nếu EXISTS:
    → Video đã upload nhưng client quên gọi /complete
    → Update: status=uploaded, size, etag, uploaded_at
    → Publish video.uploaded event (recovery path)
  
  Nếu NOT EXISTS:
    → Client không upload trong thời hạn
    → Update: status=expired
```

Tại sao cần recovery? Client có thể crash sau khi PUT lên S3 nhưng trước khi gọi `/complete`. Janitor phát hiện object đã tồn tại → tự hoàn tất thay client.

### 6.2 Dọn Session hết hạn (`runSessionBatch`)

```
SELECT * FROM upload_sessions
WHERE status IN ('initiated', 'in_progress') AND expires_at < NOW()
LIMIT {BATCH_SIZE}

Với mỗi session:
  → AbortMultipartUpload(bucket, object_key, s3_upload_id)
    (idempotent, bỏ qua lỗi 404 — S3 có thể đã tự dọn)
  → Update: status=expired
```

S3 cũng có TTL cho multipart upload (mặc định 7 ngày), nhưng janitor dọn sớm hơn để tránh tốn phí lưu trữ các parts dở.

---

## 7. Database Schema

### `videos`

```sql
id              UUID PRIMARY KEY
title           VARCHAR(255) NOT NULL
description     TEXT
owner_id        UUID                    -- nullable, người tạo
object_key      VARCHAR(512) UNIQUE     -- path trong S3
bucket          VARCHAR(128)
mime_type       VARCHAR(64)
size_bytes      BIGINT                  -- điền sau khi upload xong
etag            VARCHAR(128)            -- fingerprint từ S3
status          VARCHAR(32)             -- pending_upload | uploaded | aborted | expired
storage_url     VARCHAR(1024)           -- URL public để đọc
upload_expires_at TIMESTAMPTZ           -- deadline upload
uploaded_at     TIMESTAMPTZ             -- thời điểm xác nhận xong
```

**Index đặc biệt (partial index):**
```sql
CREATE INDEX idx_videos_pending_expiry
ON videos (upload_expires_at)
WHERE status = 'pending_upload';
```
Chỉ index các row `pending_upload` → janitor query nhanh mà không scan toàn bảng.

### `upload_sessions`

```sql
id              UUID PRIMARY KEY
video_id        UUID → videos.id (CASCADE DELETE)
s3_upload_id    VARCHAR(512)    -- ID từ S3 CreateMultipartUpload
object_key      VARCHAR(512)
bucket          VARCHAR(128)
total_parts     INT
part_size_bytes BIGINT
total_size_bytes BIGINT
status          VARCHAR(32)     -- initiated | in_progress | completing | completed | aborted | expired
expires_at      TIMESTAMPTZ
completed_at    TIMESTAMPTZ
```

### `upload_parts`

```sql
id              UUID PRIMARY KEY
session_id      UUID → upload_sessions.id (CASCADE DELETE)
part_number     INT             -- 1-indexed, khớp với S3 part number
etag            VARCHAR(128)    -- từ S3 response, cần cho CompleteMultipart
size_bytes      BIGINT
status          VARCHAR(16)     -- pending | uploaded
presign_url     TEXT
presign_expires_at TIMESTAMPTZ
uploaded_at     TIMESTAMPTZ
UNIQUE (session_id, part_number)
```

---

## 8. Event Publishing

Khi upload hoàn tất (cả simple và multipart), service publish event:

```
Exchange: video.events
Routing key: video.uploaded

Payload:
{
  "video_id":    "uuid",
  "object_key":  "videos/2026/06/uuid.mp4",
  "bucket":      "my-bucket",
  "mime_type":   "video/mp4",
  "size_bytes":  524288000,
  "etag":        "abc123",
  "uploaded_at": "2026-06-28T10:00:00Z",
  "owner_id":    "uuid",      // nullable
  "storage_url": "https://cdn.example.com/videos/2026/06/uuid.mp4"
}
```

Downstream consumers (transcoding, CDN invalidation, notification service...) subscribe exchange này.

---

## 9. Patterns & Điểm đáng chú ý

### Presigned URL — Client upload trực tiếp

Server không làm proxy. Toàn bộ bandwidth file chạy từ client → S3, không qua server. Server chỉ tốn tài nguyên cho HTTP nhỏ (metadata).

```
Không dùng presigned:  Client → Server → S3   (server bottleneck)
Dùng presigned:        Client            → S3  (server chỉ ký URL)
                       Client → Server (confirm)
```

### Optimistic Locking trên session completion

```go
// Chỉ 1 request thắng, concurrent request thứ 2 nhận rowsAffected=0 → conflict error
UPDATE upload_sessions SET status='completing' WHERE id=? AND status='in_progress'
```

Tránh gọi `CompleteMultipartUpload` hai lần (không idempotent, S3 có thể trả lỗi).

### Transaction khi tạo session + parts

```go
tx.Create(&session)           // 1 session
tx.CreateInBatches(&parts, N) // N parts
// Nếu fail → rollback cả session lẫn parts
// Đồng thời gọi AbortMultipartUpload để dọn S3
```

Đảm bảo DB và S3 không bị lệch nhau (S3 có upload nhưng DB không có session).

### HeadObject thay vì GetObject để verify

```go
// HEAD: chỉ lấy headers (size, etag), không download body
// GET: download toàn bộ file — tốn bandwidth, chậm
storage.HeadObject(ctx, bucket, key) → (size, etag, exists, err)
```

### Partial index cho janitor queries

```sql
WHERE status = 'pending_upload'  -- đã lọc bởi partial index
AND upload_expires_at < NOW()    -- index scan chỉ trên subset nhỏ
```

Không cần scan hàng triệu row `uploaded` hay `aborted`.
