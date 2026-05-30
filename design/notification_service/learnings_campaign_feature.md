# Learnings — Campaign Mass Notification Feature

Tài liệu này ghi lại những kiến thức và kỹ năng học được từ quá trình thiết kế và implement tính năng gửi notification hàng loạt cho toàn bộ user (campaign).

---

## Bối cảnh vấn đề

Gửi notification đơn lẻ (`1 event → 1 user`) là bài toán đơn giản. Campaign (`1 campaign → N triệu user`) đặt ra các vấn đề hoàn toàn khác:

| Vấn đề | Hệ quả nếu không giải quyết |
|--------|------------------------------|
| Load toàn bộ N user vào RAM | OOM, service crash |
| INSERT N rows vào DB trước khi gửi | DB timeout, I/O spike, disk đầy |
| Nhiều worker pod cùng pick campaign | Gửi duplicate cho toàn bộ user |
| Worker pod crash giữa chừng | Phải gửi lại từ đầu |
| Dispatcher nhanh hơn consumer | RabbitMQ tràn memory, broker down |

---

## Pattern 1 — Distributed Locking bằng `FOR UPDATE SKIP LOCKED`

### Vấn đề
Nhiều pod `worker-campaign` cùng chạy, cùng query `WHERE status = 'pending'` → 2 pod claim cùng 1 campaign → gửi duplicate cho toàn bộ user.

### Giải pháp

```sql
-- Trong một transaction duy nhất:
SELECT * FROM campaigns
WHERE status = 'pending'
  AND scheduled_at <= NOW()
  AND deleted_at IS NULL
ORDER BY scheduled_at ASC
LIMIT 1
FOR UPDATE SKIP LOCKED;

-- Ngay sau đó:
UPDATE campaigns SET status = 'dispatching' WHERE id = ?;
```

- `FOR UPDATE` — lock row trong transaction, pod khác không thể đọc row này cho đến khi transaction commit
- `SKIP LOCKED` — thay vì chờ lock, pod khác bỏ qua row đang bị lock và lấy row tiếp theo
- Toàn bộ wrapped trong một `Transaction` → select và update status là atomic

### Khi nào dùng pattern này
- Nhiều worker cạnh tranh nhau để lấy job từ một queue trong DB
- Muốn distributed coordination mà không cần Redis lock hay external service
- Job phải được xử lý **đúng một lần** tại một thời điểm

### Giới hạn
- Chỉ hoạt động tốt với PostgreSQL (và một số DB hỗ trợ `SKIP LOCKED`)
- Không phù hợp khi cần lock cross-database hoặc cross-service

---

## Pattern 2 — Resumable Job với Cursor Checkpoint

### V1: Offset-based (ban đầu)

```sql
-- Migration đầu tiên:
last_dispatched_offset INT NOT NULL DEFAULT 0

-- Query dispatcher:
SELECT * FROM campaign_recipients
WHERE campaign_id = ? AND status = 'pending'
ORDER BY created_at ASC, id ASC
OFFSET ? LIMIT 1000;
```

**Vấn đề với offset:** Khi `campaign_recipients` chứa hàng triệu row, `OFFSET 500000` khiến DB vẫn phải scan 500k row trước rồi mới skip. Càng về cuối càng chậm.

### V2: Cursor-based (sau refactor)

```sql
-- Migration thay thế:
ALTER TABLE campaigns
    ADD COLUMN last_dispatched_cursor VARCHAR(36) NOT NULL DEFAULT '',
    DROP COLUMN last_dispatched_offset;
```

Dispatcher không dùng offset nữa. Thay vào đó, identity service stream user theo `user_id` cursor:

```go
// Sau mỗi batch:
cursor = u.Id  // UUID của user cuối trong batch

// Resume:
w.identityClient.StreamUsers(ctx, role, cursor, batchSize, callback)
// → gRPC server filter: WHERE id > cursor ORDER BY id ASC
```

**Tại sao cursor tốt hơn offset:**

| | Offset | Cursor (UUID) |
|--|--------|---------------|
| DB work | Scan N rows trước khi skip | Seek thẳng qua index |
| Performance | O(N) — chậm dần | O(log N) — không đổi |
| Crash safety | Resume đúng vị trí | Resume đúng vị trí |
| Insert mới | Có thể skip/duplicate row | Không ảnh hưởng |

### Checkpoint đúng thời điểm

```go
// ĐÚNG: checkpoint SAU KHI publish thành công
if err := w.publishBatch(ctx, campaign, batch); err != nil {
    return err  // không checkpoint nếu publish thất bại
}
w.repo.CheckpointDispatch(ctx, campaign.Id, cursor, len(batch))

// SAI: checkpoint trước khi publish
// → crash sau checkpoint nhưng trước publish → silent data loss
```

**Trade-off at-least-once:** Nếu crash sau publish nhưng trước checkpoint → batch đó sẽ bị publish lại khi resume → **duplicate**. Đây là acceptable trade-off cho at-least-once delivery. Để đạt exactly-once cần idempotency key ở worker.

---

## Pattern 3 — gRPC Server-Side Streaming cho Large Dataset

### Vấn đề
Notification service cần lấy toàn bộ N user từ Identity service. REST API không phù hợp:
- Một request trả về 1M user → timeout, OOM cả 2 phía
- Phân trang REST → N round-trip, N kết nối HTTP

### Giải pháp

Identity service expose gRPC server-side streaming:

```go
// Notification worker gọi:
w.identityClient.StreamUsers(ctx, role, cursor, batchSize,
    func(u *UserRecord) error {
        batch = append(batch, u)
        if len(batch) >= batchSize {
            publishBatch(batch)
            checkpoint(cursor)
            batch = batch[:0]
        }
        cursor = u.Id
        return nil
    },
)
```

Identity service stream từng `UserRecord` qua một kết nối gRPC duy nhất. Worker tích lũy vào batch rồi flush — **memory footprint không tăng theo số user**.

### Khi nào dùng gRPC streaming
- Consumer cần xử lý **toàn bộ** dataset, không chỉ một phần
- Dataset quá lớn để fit vào một response
- Cần streaming backpressure tự nhiên (consumer xử lý bao nhiêu thì server gửi bấy nhiêu)

---

## Pattern 4 — Back-pressure 3 Lớp

### Vấn đề
Dispatcher publish 1M message vào RabbitMQ nhanh hơn worker-email có thể xử lý → queue tràn → broker page sang disk → latency tăng → cuối cùng broker down.

### 3 lớp bảo vệ

**Lớp 1 — Queue hard cap (RabbitMQ):**

```go
var campaignEmailQueueArgs = map[string]interface{}{
    "x-max-length": int32(50000),      // tối đa 50k message pending
    "x-overflow":   "reject-publish",  // NACK thay vì drop silently
}
```

Khi queue đầy, RabbitMQ trả NACK về dispatcher thay vì drop message âm thầm. Publisher biết ngay.

**Lớp 2 — Exponential backoff tại publisher:**

```go
func (w *CampaignWorker) publishBatch(...) error {
    const maxRetries = 5
    for attempt := 0; attempt < maxRetries; attempt++ {
        err := w.tryPublishBatch(ctx, campaign, users)
        if err == nil {
            return nil
        }
        if errors.Is(err, broker.ErrQueueFull) {
            select {
            case <-time.After(time.Duration(attempt+1) * 5 * time.Second):
                // backoff: 5s → 10s → 15s → 20s → 25s
            case <-ctx.Done():
                return ctx.Err()
            }
            continue
        }
        return err
    }
}
```

**Lớp 3 — Batch size giới hạn memory tại worker:**

Không stream toàn bộ user vào slice trước khi publish. Tích lũy đến `batchSize` rồi flush ngay:

```go
if len(batch) < batchSize {
    return nil  // tiếp tục nhận
}
// flush
publishBatch(batch)
batch = batch[:0]  // reset, không alloc lại
```

### Kết quả

```
Dispatcher ──► RabbitMQ [max 50k] ──► worker-email (N pods)
    │               │
    │  NACK (full)  │
    └── backoff ────┘
```

Memory của campaign worker flat dù N user tăng lên. Queue không bao giờ vượt 50k.

---

## Pattern 5 — Audit Log Insert After, Not Before

### Vấn đề
Khi nào insert `notification` record vào DB — trước hay sau khi gửi email?

**Insert trước:**
```
Insert notification (status=pending) → Send SMTP → Update (status=sent)
```
Nếu crash sau insert nhưng trước send → record treo ở `pending` mãi, cần retry job phức tạp.

**Insert sau (cách đã chọn):**
```
Send SMTP → Insert notification (status=sent/failed)
```

```go
// Worker-email:
err := w.sendEmail(...)
if err != nil {
    w.insertCampaignAudit(ctx, task, domain.NotificationStatusFailed, err.Error())
    return err
}
// Chỉ insert SAU KHI biết kết quả
w.insertCampaignAudit(ctx, task, domain.NotificationStatusSent, "")
```

**Trade-off:** Nếu crash sau send nhưng trước insert → audit record bị mất (sent nhưng không có log). Acceptable cho audit log, không acceptable cho billing.

---

## Design Decision — Tách `campaign_recipients` vs Dùng Identity Service

### V1 (design doc ban đầu)
API nhận `recipients[]` trong request body → insert vào `campaign_recipients` table → dispatcher đọc từ đó.

**Vấn đề:** Người dùng phải tự truyền danh sách user. Với 1 triệu user thì request body là vài trăm MB.

### V2 (implementation thực tế)
API chỉ nhận `filter` (ví dụ `{"role": "user"}`) → lưu `filter_criteria` JSON vào campaign row → dispatcher hỏi Identity service lúc dispatch.

```go
// Tại dispatch time, không phải create time:
total, _ := w.identityClient.CountUsers(ctx, filter.Role)
w.repo.SetTotalRecipients(ctx, campaign.Id, total)

w.identityClient.StreamUsers(ctx, filter.Role, cursor, batchSize, callback)
```

**Lợi ích:**
- Request tạo campaign nhỏ gọn
- `total_recipients` phản ánh đúng số user tại thời điểm dispatch (user mới register vẫn được tính)
- Không cần bảng `campaign_recipients` trung gian

**Đánh đổi:** Số user có thể thay đổi giữa lúc tạo campaign và lúc dispatch.

---

## Design Decision — `last_dispatched_offset` → `last_dispatched_cursor`

Migration [20260530000000_replace_offset_with_cursor_on_campaigns.sql](../../services/notification/migrations/20260530000000_replace_offset_with_cursor_on_campaigns.sql) thay thế hoàn toàn:

```sql
ALTER TABLE campaigns
    ADD COLUMN last_dispatched_cursor VARCHAR(36) NOT NULL DEFAULT '',
    DROP COLUMN last_dispatched_offset;
```

**Lý do thay đổi:**
- Offset pagination trên `campaign_recipients` chậm dần khi table lớn (`OFFSET N` vẫn scan N rows)
- Identity service đã có gRPC streaming với cursor — tận dụng luôn, không cần lưu recipients trong notification DB
- Cursor là UUID của user cuối cùng được dispatch — seek O(log N) qua index thay vì O(N) scan

---

## Schema Evolution — Bài học về Migration

Campaign feature đi qua 3 migration:

| Migration | Thay đổi | Lý do |
|-----------|---------|-------|
| `20260512_add_mass_campaign` | Tạo `campaigns` + `campaign_recipients`, `last_dispatched_offset` | Bootstrap |
| `20260529_add_filter_criteria` | Thêm `filter_criteria`, `target_audience` | Thay recipients list bằng filter |
| `20260530_replace_offset_with_cursor` | Drop `last_dispatched_offset`, add `last_dispatched_cursor` | Performance |

**Bài học:** Không cần thiết kế hoàn hảo từ đầu. Migration cho phép evolve schema khi hiểu rõ hơn về vấn đề. Quan trọng là mỗi migration có `-- +goose Down` để rollback.

---

## Tóm tắt các kỹ năng học được

```
1. FOR UPDATE SKIP LOCKED     → distributed worker coordination trong PostgreSQL
2. Cursor checkpoint           → resumable long-running job, không mất tiến độ khi crash
3. Cursor vs Offset pagination → cursor O(log N) hiệu quả hơn offset O(N) cho large table
4. gRPC server-side streaming  → xử lý large dataset cross-service không bị memory bound
5. Back-pressure 3 lớp         → queue cap + publisher backoff + batch size
6. Audit insert after send     → đơn giản hóa state machine, trade-off mất log khi crash
7. Filter-based audience       → lazy evaluation tại dispatch time thay vì eager pre-compute
8. Schema evolution via goose  → migrate dần theo hiểu biết, không cần big-bang design
```
