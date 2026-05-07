# Hướng dẫn implement Circuit Breaker cho SMTP

## Bối cảnh

Hiện tại `EmailWorker.sendEmail()` trong `services/notification/internal/worker/email_worker.go` gọi thẳng `smtp.SendMail()` — nếu SMTP provider down, mọi message vẫn cứ thử gửi, tốn tài nguyên và tắc queue. Circuit breaker sẽ "ngắt mạch" sau N lần fail liên tiếp, bảo vệ toàn bộ pipeline.

---

## Bước 1 — Tạo file `circuit_breaker.go`

Tạo file mới tại `services/notification/internal/worker/circuit_breaker.go`. Implement **state machine thuần Go, không dùng thư viện ngoài**.

**Struct cần định nghĩa:**

```go
type State int

const (
    StateClosed   State = iota // hoạt động bình thường
    StateOpen                  // chặn toàn bộ request
    StateHalfOpen              // thử 1 request để kiểm tra
)

type CircuitBreaker struct {
    // config
    maxFailures  int           // số lần fail liên tiếp để OPEN
    openDuration time.Duration // thời gian giữ OPEN trước khi thử HALF-OPEN

    // state (cần mutex vì nhiều goroutine có thể đọc/ghi)
    mu           sync.Mutex
    state        State
    failures     int       // đếm fail liên tiếp hiện tại
    lastFailTime time.Time // thời điểm fail cuối cùng (để tính khi nào hết OPEN)
}
```

**Constructor:**

```go
func NewCircuitBreaker(maxFailures int, openDuration time.Duration) *CircuitBreaker
```

**Method duy nhất cần export:**

```go
// Execute chạy fn trong vòng bảo vệ của circuit breaker.
// Trả về ErrCircuitOpen nếu circuit đang OPEN.
func (cb *CircuitBreaker) Execute(fn func() error) error
```

**Logic bên trong `Execute`:**

```
Lock mutex
  - Nếu OPEN: kiểm tra xem đã hết openDuration chưa
      + Chưa hết → unlock, trả ErrCircuitOpen ngay
      + Hết rồi  → chuyển sang HALF-OPEN
  - Nếu HALF-OPEN: dùng flag halfOpenInFlight bool
      + Nếu đang có request thử → trả ErrCircuitOpen
      + Chưa có → set halfOpenInFlight = true, cho qua
Unlock mutex

Gọi fn()

Lock mutex lại
  - Nếu fn() thành công:
      + Reset failures = 0, halfOpenInFlight = false
      + Chuyển về CLOSED
  - Nếu fn() thất bại:
      + Tăng failures++
      + Cập nhật lastFailTime = now
      + halfOpenInFlight = false
      + Nếu failures >= maxFailures → chuyển sang OPEN
Unlock mutex

Trả kết quả
```

**Thêm method để expose state (dùng cho health endpoint):**

```go
func (cb *CircuitBreaker) State() State
func (cb *CircuitBreaker) Failures() int
```

---

## Bước 2 — Tích hợp vào `EmailWorker`

Sửa `services/notification/internal/worker/email_worker.go`:

**Thêm field vào struct:**

```go
type EmailWorker struct {
    repo    repository.NotificationRepository
    broker  broker.Broker
    cfg     *config.Config
    breaker *CircuitBreaker  // thêm dòng này
}
```

**Sửa constructor `NewEmailWorker`:**

```go
func NewEmailWorker(repo ..., cfg *config.Config) *EmailWorker {
    return &EmailWorker{
        ...
        breaker: NewCircuitBreaker(
            cfg.Worker.CircuitBreakerMaxFailures,
            cfg.Worker.CircuitBreakerOpenDuration,
        ),
    }
}
```

**Sửa hàm `sendEmail` — wrap SMTP call:**

```go
func (w *EmailWorker) sendEmail(id, to, subject, body string) error {
    return w.breaker.Execute(func() error {
        smtpCfg := w.cfg.SMTP
        // ... giữ nguyên code smtp.SendMail hiện tại ...
        return smtp.SendMail(addr, auth, smtpCfg.From, []string{to}, msg)
    })
}
```

**Xử lý `ErrCircuitOpen` trong message handler:**

```go
err := w.sendEmail(task.ID, task.To, task.Subject, task.Body)
if errors.Is(err, ErrCircuitOpen) {
    // NACK — trả message về queue, không tính là retry lần nào
    log.Warn("circuit breaker open, NACK message back to queue",
        zap.String("notification_id", task.ID))
    msg.Nack(false, true) // requeue=true
    return
}
// xử lý err thường như hiện tại...
```

> **Tại sao NACK thay vì ACK?** Khi circuit OPEN, ta chưa thử gửi — message không nên bị mất. NACK với `requeue=true` trả về queue, khi circuit recover thì worker sẽ pick lại.

---

## Bước 3 — Thêm config

Sửa `services/notification/internal/config/config.go`:

```go
type WorkerConfig struct {
    // ... fields hiện có ...
    CircuitBreakerMaxFailures  int           `mapstructure:"circuit_breaker_max_failures"`
    CircuitBreakerOpenDuration time.Duration `mapstructure:"circuit_breaker_open_duration"`
}
```

Thêm vào `.env.example`:

```
WORKER_CIRCUIT_BREAKER_MAX_FAILURES=5
WORKER_CIRCUIT_BREAKER_OPEN_DURATION=30s
```

Mặc định hợp lý nếu env không set: `maxFailures=5`, `openDuration=30s`.

---

## Bước 4 — Expose state qua `/health`

Sửa health handler để trả thêm thông tin circuit breaker:

```json
{
  "status": "ok",
  "smtp_circuit": {
    "state": "open",
    "failures": 5
  }
}
```

Để truyền `*CircuitBreaker` vào handler, có hai cách:

- **Cách đơn giản:** thêm field `breaker *CircuitBreaker` vào `NotificationHandler`, inject khi khởi tạo trong `cmd/main.go`
- **Cách tách biệt:** tạo `/health` endpoint riêng cho worker mode (không chạy HTTP server đầy đủ, chỉ expose `/health` trên port phụ)

---

## Bước 5 — Viết unit test

Tạo file `circuit_breaker_test.go` cùng thư mục. Cần cover **4 scenario**:

| Test case | Mô tả |
|-----------|-------|
| `TestCircuitBreaker_StaysClosed` | Dưới ngưỡng fail → vẫn CLOSED |
| `TestCircuitBreaker_OpensAfterMaxFailures` | Đúng N fail → chuyển OPEN, trả `ErrCircuitOpen` ngay |
| `TestCircuitBreaker_HalfOpenAfterDuration` | Sau `openDuration` → HALF-OPEN, cho 1 request qua |
| `TestCircuitBreaker_ClosesOnSuccess` | HALF-OPEN + success → về CLOSED, reset failures |

Để test transition thời gian, dùng duration ngắn:

```go
cb := NewCircuitBreaker(3, 1*time.Millisecond)
// trigger OPEN bằng 3 lần fail
// time.Sleep(2 * time.Millisecond)
// gọi lại → phải là HALF-OPEN
```

---

## Sơ đồ state machine

```
           maxFailures fail liên tiếp
 CLOSED ──────────────────────────────► OPEN
   ▲                                      │
   │                                      │ openDuration hết
   │                                      ▼
   │            success               HALF-OPEN
   └──────────────────────────────────────┘
         (fail ở HALF-OPEN → quay lại OPEN, reset timer)
```

---

## Checklist để biết mình đã implement đúng

- [ ] `Execute()` thread-safe (dùng `sync.Mutex`, không deadlock)
- [ ] OPEN không gọi `fn()` gì cả — trả lỗi ngay lập tức
- [ ] HALF-OPEN chỉ cho **1** request qua, không phải tất cả
- [ ] Fail trong HALF-OPEN → reset timer OPEN (không về CLOSED)
- [ ] Success trong HALF-OPEN → reset `failures = 0` về CLOSED
- [ ] `ErrCircuitOpen` được NACK đúng cách, không bị ACK mất message
- [ ] Config đọc từ env, có default value nếu không set
- [ ] Unit test cover đủ 4 scenario transition

---

## Thứ tự thực hiện khuyến nghị

1. Viết và test `circuit_breaker.go` độc lập trước (không phụ thuộc gì)
2. Khi test pass hết → tích hợp vào `email_worker.go`
3. Thêm config fields và env vars
4. Thêm state vào health endpoint
