# Observing & Scaling — Notification Service

Hướng dẫn thực hành: cách đọc metric để biết hệ thống đang đứng ở đâu, khi nào cần scale, và scale như thế nào.

---

## Kiến trúc và điểm bottleneck

```
Campaign API ──► campaign_worker (1 pod, FOR UPDATE SKIP LOCKED)
                      │
                      ▼ gRPC stream (batch_size users/lần)
              identity-service:50051
                      │
                      ▼ publish → campaign.email queue (max 50 000 msg)
              RabbitMQ [notification.direct exchange]
                      │
                      ├──► worker-email (N pods) ──► SMTP
                      └──► worker-webhook (N pods) ──► HTTP endpoint
```

**Bottleneck theo thứ tự thường gặp:**

| # | Bottleneck | Signal |
|---|-----------|--------|
| 1 | `worker-email` quá ít pod | Queue depth tăng, backoff tăng |
| 2 | SMTP server chậm / từ chối | `email_send_duration_seconds` tăng, circuit breaker mở |
| 3 | `campaign_worker` gRPC stream chậm | `campaign_users_dispatched_total` rate thấp |
| 4 | RabbitMQ broker quá tải | Consumer count = 0 hoặc delivery rate = 0 |

---

## Layer 1 — Prometheus + Grafana (số liệu chính xác nhất)

**URL:** `http://localhost/grafana` (local) | `http://localhost:3000` (k8s port-forward)  
**Datasource:** Prometheus (đã configure trong `grafana-prometheus-datasource.yaml`)

Notification service expose `/metrics` ở port 8082.

### 1.1 Throughput email — phát hiện bottleneck chính

```promql
# Tốc độ email gửi thành công (email/giây, phân theo loại)
sum(rate(notification_emails_processed_total{status="success"}[2m])) by (type)
```
- `type=campaign` — campaign emails được xử lý bởi `HandleCampaignMessage`
- `type=regular` — notification thường qua `HandleMessage`

```promql
# So sánh success vs failed — phát hiện lỗi SMTP
sum(rate(notification_emails_processed_total[2m])) by (type, status)
```

**Đọc kết quả:**
- `status=failed` tăng đột ngột → SMTP server có vấn đề
- `status=circuit_open` xuất hiện → circuit breaker đang mở (SMTP unreachable)
- `status=retried` cao → email thất bại nhiều lần, xem lại SMTP config

### 1.2 Latency SMTP — phát hiện SMTP chậm

```promql
# p50 latency gửi email (median)
histogram_quantile(0.50, rate(notification_email_send_duration_seconds_bucket[5m]))

# p99 latency — worst case
histogram_quantile(0.99, rate(notification_email_send_duration_seconds_bucket[5m]))

# Phân theo loại (regular vs campaign)
histogram_quantile(0.99, rate(notification_email_send_duration_seconds_bucket[5m])) by (type)
```

**Ngưỡng tham chiếu:**
- p99 < 2s → bình thường (Mailpit local)
- p99 2–5s → SMTP chậm, xem xét tăng timeout
- p99 > 5s → circuit breaker sẽ mở, cần điều tra SMTP

### 1.3 Campaign dispatch rate — đo tốc độ publish

```promql
# Số user/giây đang được dispatch vào queue
rate(notification_campaign_users_dispatched_total[2m])

# Số batch/phút
rate(notification_campaign_batches_dispatched_total[2m]) * 60
```

Với `WORKER_CAMPAIGN_BATCH_SIZE=1000`:
- 1 batch/phút = 1000 user/phút → campaign 50 000 user mất ~50 phút
- 6 batch/phút = 6000 user/phút → campaign 50 000 user mất ~8 phút

### 1.4 Back-pressure — queue có đang đầy không?

```promql
# Số lần campaign_worker bị block vì queue đầy (mỗi backoff = 5s delay)
rate(notification_campaign_backoffs_total[5m])
```

- Rate = 0 → queue không đầy, worker-email đủ capacity
- Rate > 0 → queue đang đầy (≥50 000 messages), **cần scale thêm worker-email**
- Rate tăng dần → worker-email đang càng ngày càng chậm hơn dispatch rate

### 1.5 Campaign dispatch duration — đo tổng thời gian một campaign

```promql
# Median thời gian hoàn thành một campaign (từ start đến fully dispatched)
histogram_quantile(0.50, rate(notification_campaign_dispatch_duration_seconds_bucket[30m]))

# p95
histogram_quantile(0.95, rate(notification_campaign_dispatch_duration_seconds_bucket[30m]))
```

### 1.6 HTTP API — kiểm tra API endpoint

```promql
# Request rate của notification API
sum(rate(notification_http_requests_total[2m])) by (method, path, status)

# Tỉ lệ lỗi (4xx, 5xx)
sum(rate(notification_http_requests_total{status=~"[45].."}[2m]))
  /
sum(rate(notification_http_requests_total[2m]))

# p99 latency API
histogram_quantile(0.99, rate(notification_http_request_duration_seconds_bucket[5m])) by (path)

# Request đang xử lý (in-flight)
notification_http_requests_in_flight
```

---

## Layer 2 — RabbitMQ Management UI (real-time queue depth)

**URL:** `http://rabbitmq.localhost` (k8s) | `http://localhost:15672` (local)  
**Login:** `guest` / `guest`

### Queue cần theo dõi

Vào tab **Queues** và nhìn vào `campaign.email`:

| Metric | Ý nghĩa | Action |
|--------|---------|--------|
| **Messages ready** | Đang chờ chưa được xử lý | >10 000 → scale worker-email |
| **Publish rate (msg/s)** | Tốc độ campaign_worker đẩy vào | Baseline để so sánh |
| **Deliver rate (msg/s)** | Tốc độ worker-email xử lý | Phải gần bằng publish rate |
| **Consumer count** | Số connection đang consume | 0 = worker-email đã chết |
| **Unacked** | Message đang được xử lý chưa ACK | Tăng cao = worker bị treo |

**Đọc kết quả:**
```
Publish rate > Deliver rate  →  worker-email là bottleneck → scale thêm replica
Deliver rate > Publish rate  →  queue đang drain → đủ capacity
Messages ready = 50 000      →  queue đầy, campaign_worker đang backoff
Consumer count tăng 3x       →  Deliver rate nên tăng ~3x (lý thuyết)
Unacked tăng không ngừng     →  worker bị stuck, xem logs
```

### Queue khác cần biết

- **`email`** — regular notifications (có DLQ via `x-dead-letter-exchange`)
- **`email.retry`** — messages đang chờ retry (TTL 1–15 phút tùy attempt)
- **`email.dlq`** — dead-letter queue, message đã fail hết số lần retry

---

## Layer 3 — Loki Logs (drill down khi có vấn đề)

**Grafana → Explore → Datasource: Loki**

### LogQL queries quan trọng

**Theo dõi tiến độ dispatch:**
```logql
{service="notification-service"} |= "batch dispatched"
```
Mỗi dòng = 1 batch đã publish thành công. Đếm rate để so với số user cần gửi.

**Phát hiện backoff (queue đầy):**
```logql
{service="notification-service"} |= "campaign queue full, backing off"
```
Xuất hiện log này → queue đã đạt 50 000 message, cần scale worker-email.

**Theo dõi circuit breaker:**
```logql
{service="notification-service"} |= "circuit breaker"
```

**Phát hiện lỗi gửi email:**
```logql
{service="notification-service"} | json | level="error" |= "email"
```

**Tổng hợp error rate (Loki metric query):**
```logql
# Dùng trong Grafana panel khi Prometheus chưa có hoặc cần cross-check
rate({service="notification-service"} | json | level="error" [1m])
```

---

## Decision Framework — Khi nào scale?

```
Câu hỏi 1: campaign.email "Messages ready" có tăng không?
│
├── KHÔNG tăng (queue flat hoặc giảm)
│     → Hệ thống đủ capacity. Không cần scale.
│
└── CÓ tăng liên tục
      │
      ├── Câu hỏi 2: notification_campaign_backoffs_total rate > 0?
      │     │
      │     ├── CÓ → queue đầy, dispatch đang bị block
      │     │         → Scale worker-email trước (xem Section 4)
      │     │
      │     └── KHÔNG → queue chưa đầy nhưng vẫn tăng
      │                   → Đây là normal lag, chờ thêm 2-3 phút
      │
      ├── Câu hỏi 3: email_send_duration_seconds p99 > 5s?
      │     → SMTP chậm, không phải thiếu worker
      │       → Xem circuit breaker logs, kiểm tra SMTP server
      │
      └── Câu hỏi 4: Consumer count = 0 trên queue?
            → Worker-email đã crash
              → kubectl logs + kubectl rollout restart
```

---

## Scale Commands

### Scale worker-email (bottleneck chính)

```bash
# Xem trạng thái hiện tại
kubectl get pods -n microservices-platform | grep notification

# Scale lên 3 replica
kubectl scale deployment notification-worker-email \
  --replicas=3 -n microservices-platform

# Theo dõi pod mới khởi động
kubectl get pods -n microservices-platform -w | grep worker-email
```

**Kỳ vọng sau scale:** RabbitMQ Deliver rate tăng gần 3x, Messages ready giảm dần, `notification_emails_processed_total` rate tăng tương ứng.

**Giới hạn của horizontal scale worker-email:**
- Mỗi pod consume với `prefetch=20` (config `WORKER_CAMPAIGN_PREFETCH`)
- 3 pods → 60 messages xử lý song song tối đa
- Nếu SMTP có rate limit, tăng pod không giúp được — phải xem lại SMTP config

### Scale campaign worker (khi có nhiều campaign song song)

```bash
kubectl scale deployment notification-worker-campaign \
  --replicas=3 -n microservices-platform
```

`FOR UPDATE SKIP LOCKED` đảm bảo 3 pod không claim cùng 1 campaign. Hiệu quả chỉ khi có ≥3 campaign đang `pending` cùng lúc.

### Xem resource usage

```bash
kubectl top pods -n microservices-platform
```

Nếu worker-email CPU/Memory gần limit → tăng resource requests trong `k8s/services/notification/09_notification.yaml` thay vì chỉ tăng replica.

---

## Theo dõi campaign đang chạy (API endpoint)

```bash
# Lấy campaign_id từ response của POST /api/v1/campaigns
# Sau đó:
curl http://localhost:8082/api/v1/campaigns/{campaign_id}/stats
```

Response:
```json
{
  "dispatched_count": 15000,
  "sent_count": 14850,
  "failed_count": 150,
  "last_dispatched_cursor": "user-uuid-xxx",
  "total_recipients": 50000,
  "progress_pct": 30.0
}
```

| Field | Ý nghĩa |
|-------|---------|
| `dispatched_count` | Số user đã publish vào RabbitMQ |
| `sent_count` | Số email đã gửi thành công (worker xác nhận) |
| `failed_count` | Số email thất bại |
| `last_dispatched_cursor` | UUID user cuối cùng → resume point sau crash |
| `progress_pct` | `dispatched_count / total_recipients * 100` |

`sent_count` thường lag sau `dispatched_count` vì worker-email xử lý async.

---

## Checklist thực hành khi chạy campaign test

```
[ ] Mở RabbitMQ UI (rabbitmq.localhost) → tab Queues → campaign.email
[ ] Mở Grafana → Explore → Loki → filter: {service="notification-service"}
[ ] Mở terminal: kubectl top pods -n microservices-platform -w

[ ] POST /api/v1/campaigns → lưu campaign_id
[ ] Sau 30s: kiểm tra campaign.email "Messages ready" có tăng không
[ ] Sau 60s: GET /api/v1/campaigns/{id}/stats → dispatched_count có tăng không

[ ] Grafana/Prometheus: rate(notification_campaign_users_dispatched_total[2m])
[ ] Grafana/Prometheus: rate(notification_emails_processed_total{status="success"}[2m])

[ ] Nếu backoff log xuất hiện → scale worker-email lên 3
[ ] So sánh Deliver rate trước và sau scale
[ ] So sánh rate(notification_emails_processed_total[2m]) trước và sau scale

[ ] Kill campaign pod → xác nhận stats resume từ last_dispatched_cursor
```

---

## Tổng kết — 3 số quan trọng nhất

Khi cần đánh giá nhanh hệ thống trong 60 giây, nhìn 3 số này:

1. **RabbitMQ `campaign.email` Messages ready** — tăng liên tục = có vấn đề
2. **`rate(notification_emails_processed_total{status="success"}[2m])`** — throughput thực của hệ thống
3. **`rate(notification_campaign_backoffs_total[2m])`** — queue có đầy không, cần scale ngay không
