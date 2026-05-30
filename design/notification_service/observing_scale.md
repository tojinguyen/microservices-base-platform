# Observing Scale — Notification Service

Tài liệu này tổng hợp kiến thức và kỹ năng quan sát khả năng scale của hệ thống notification, dựa trên kiến trúc thực tế của project.

---

## Tổng quan kiến trúc liên quan đến scale

```
Campaign API ──► campaign_worker (1 pod, FOR UPDATE SKIP LOCKED)
                      │
                      ▼ gRPC stream (1000 users/batch)
              identity-service:50051
                      │
                      ▼ publish
              RabbitMQ [campaign.email queue, max 50000]
                      │
                      ▼ consume
          worker-email (N pods) ──► SMTP (Mailpit)
```

Điểm bottleneck tiềm năng:
- `worker-email`: số pod quyết định throughput email/giây
- RabbitMQ queue depth: phản ánh chênh lệch tốc độ publisher vs consumer
- gRPC stream từ identity: tốc độ đọc user list

---

## 1. RabbitMQ Management UI

**Địa chỉ:** `http://localhost:15672` (local) | `http://rabbitmq.localhost` (k8s nếu có ingress)  
**Đây là nơi quan sát scale trực tiếp và nhanh nhất.**

### Các metric cần nhìn khi campaign chạy

| Tab | Metric | Ý nghĩa |
|-----|--------|---------|
| Queues → `campaign.email` | **Messages ready** | Số message đang chờ chưa được xử lý. Tăng không ngừng = worker không đủ |
| Queues → `campaign.email` | **Publish rate (msg/s)** | Tốc độ campaign_worker đẩy vào queue |
| Queues → `campaign.email` | **Deliver rate (msg/s)** | Tốc độ worker-email xử lý. So sánh với publish rate |
| Queues → `campaign.email` | **Consumer count** | Số worker đang active. Scale lên → số này tăng |
| Overview | **Memory / Disk** | RabbitMQ broker health |

### Đọc kết quả

```
Publish rate > Deliver rate  →  worker-email là bottleneck → scale thêm replica
Publish rate ≈ Deliver rate  →  hệ thống cân bằng
Messages ready → 50000       →  queue đầy, dispatcher bắt đầu backoff (thiết kế đúng)
Consumer count tăng 3x       →  Deliver rate nên tăng ~3x nếu scale hiệu quả
```

---

## 2. Loki + Grafana (Structured Logs)

**Cài đặt:** `make loki-install && make dashboard-apply`  
**Grafana:** `http://localhost:3000`

### LogQL queries hữu ích

**Theo dõi tiến độ campaign dispatch:**
```logql
{service="notification-service"} |= "batch dispatched"
```
Mỗi dòng log này = 1 batch (mặc định 1000 user) đã được publish vào RabbitMQ. Đếm số dòng/phút để tính throughput.

**Phát hiện lỗi trong campaign:**
```logql
{service="notification-service"} |= "campaign" | json | level="error"
```

**Theo dõi backoff khi queue đầy:**
```logql
{service="notification-service"} |= "campaign queue full"
```
Xuất hiện log này = queue đã đạt giới hạn 50000, back-pressure đang hoạt động đúng.

**Theo dõi worker-email gửi thành công:**
```logql
{service="notification-service"} |= "email sent"
```

### Dashboard có sẵn

File [grafana-logs-dashboard.yaml](../../k8s/monitoring/grafana-logs-dashboard.yaml) deploy dashboard `Microservices Logs` với filter theo:
- `service`: chọn `notification-service`
- `level`: filter `error` / `warn` / `info`
- `trace_id`: drill down theo request cụ thể

---

## 3. Jaeger — Distributed Tracing

**OTEL đã được instrument:** `OTEL_ENABLED=true`, exporter → `jaeger-service:4318`

### Span đã có trong campaign

```
campaign.dispatch (root span)
  │  attributes: campaign.id, campaign.total_recipients
  └─► gRPC StreamUsers
  └─► publishBatch (mỗi batch)
  └─► CheckpointDispatch
```

### Cách dùng

1. Mở Jaeger UI (nếu deploy): `http://jaeger.localhost`
2. Service: `notification-service`
3. Operation: `campaign.dispatch`
4. So sánh latency giữa các lần chạy với số user khác nhau

### Metric cần đo

| Measurement | Cách đo |
|-------------|---------|
| Thời gian dispatch toàn bộ campaign | Duration của span `campaign.dispatch` |
| Thời gian mỗi batch gRPC | Child span của StreamUsers |
| Số lần retry publish | Tìm log "backing off" trong span |

---

## 4. Scale thực tế — Kubectl Commands

### Xem trạng thái hiện tại

```bash
kubectl get pods -n microservices-platform | grep notification
```

Output mẫu:
```
notification-api-xxx              1/1   Running
notification-worker-email-xxx     1/1   Running   ← bottleneck
notification-worker-campaign-xxx  1/1   Running
notification-worker-outbox-xxx    2/2   Running
```

### Scale worker-email (bottleneck chính)

```bash
# Scale lên 3 replica
kubectl scale deployment notification-worker-email \
  --replicas=3 -n microservices-platform

# Theo dõi pod khởi động
kubectl get pods -n microservices-platform -w | grep worker-email
```

**Kỳ vọng sau scale:** RabbitMQ Deliver rate tăng ~3x, Messages ready giảm dần.

### Scale campaign worker (khi có nhiều campaign song song)

```bash
kubectl scale deployment notification-worker-campaign \
  --replicas=3 -n microservices-platform
```

`FOR UPDATE SKIP LOCKED` đảm bảo 3 pod không claim cùng 1 campaign. Hiệu quả khi có ≥3 campaign đang `pending`.

### Xem resource usage

```bash
kubectl top pods -n microservices-platform
```

Nếu worker-email CPU/Memory gần limit → tăng resources trong [09_notification.yaml](../../k8s/services/notification/09_notification.yaml) hoặc scale thêm pod.

---

## 5. Cursor Checkpoint — Quan sát Resume sau Crash

Khi campaign đang chạy mà pod bị kill, kiểm tra checkpoint:

```bash
# Lấy campaign_id từ response của POST /api/v1/campaigns
curl http://localhost:8082/api/v1/campaigns/{campaign_id}/stats
```

Response:
```json
{
  "dispatched_count": 15000,
  "last_dispatched_cursor": "user-uuid-xxx",
  "total_recipients": 50000,
  "progress_pct": 30.0
}
```

- `dispatched_count` tăng đều = dispatch đang chạy tốt
- `last_dispatched_cursor` thay đổi sau mỗi batch = checkpoint hoạt động
- Sau khi pod crash và restart, `dispatched_count` tiếp tục từ giá trị cũ, không reset về 0

---

## 6. Back-pressure — Xác nhận 3 lớp hoạt động

### Lớp 1: RabbitMQ queue cap
Kiểm tra qua Management UI — khi `Messages ready = 50000`, queue từ chối publish mới (`x-overflow: reject-publish`).

### Lớp 2: Publisher exponential backoff
Log xuất hiện trong Loki:
```
"campaign queue full, backing off" attempt=1
"campaign queue full, backing off" attempt=2
...
```
Interval tăng dần: 5s → 10s → 15s → 20s → 25s (tối đa 5 lần).

### Lớp 3: Batch size giới hạn memory
Config `WORKER_CAMPAIGN_BATCH_SIZE=1000` (default). Worker không load toàn bộ user vào memory — chỉ giữ tối đa 1000 UserRecord tại một thời điểm.

Kiểm tra: `kubectl top pods` — memory của campaign worker phải flat dù số user tăng.

---

## 7. Gap hiện tại — Chưa có Prometheus Metrics

Notification service **không expose `/metrics`** (khác với identity-service đã có). Do đó không thể:
- Vẽ graph email/giây theo thời gian trong Grafana
- Alert khi error rate vượt ngưỡng
- Tính p99 latency của từng worker

**Workaround hiện tại:** Dùng Loki log-based metrics (rate của log lines) thay cho Prometheus counter.

```logql
# Email sent rate per minute
rate({service="notification-service"} |= "email sent" [1m])
```

---

## Checklist quan sát khi test campaign

```
[ ] RabbitMQ UI mở sẵn, nhìn vào queue campaign.email
[ ] Loki filter: {service="notification-service"} |= "batch dispatched"
[ ] kubectl top pods đang chạy ở terminal khác
[ ] POST /api/v1/campaigns → lưu campaign_id
[ ] Sau 30s: GET /api/v1/campaigns/{id}/stats → kiểm tra dispatched_count tăng
[ ] Scale worker-email lên 3 → so sánh Deliver rate trước/sau
[ ] Kill campaign pod → xác nhận stats resume từ last_dispatched_cursor
```
