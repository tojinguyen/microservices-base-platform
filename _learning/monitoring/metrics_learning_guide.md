# Hướng dẫn học tập & Triển khai Metrics Monitoring

Tài liệu này tổng hợp các kiến thức cốt lõi, công cụ cần thiết, các metric quan trọng và các tình huống debug thực tế bằng metrics để bạn dễ dàng nắm bắt và áp dụng cho hệ thống microservices.

## 1. Những kiến thức cốt lõi cần học
- **Các loại Metrics cơ bản:** Cần hiểu sự khác biệt và ứng dụng của `Counter` (chỉ tăng), `Gauge` (tăng/giảm), `Histogram` (phân phối theo nhóm/bucket, tính percentile), `Summary`.
- **RED Method (Dành cho Services):**
  - **R**ate (Số lượng request trên giây).
  - **E**rrors (Tỉ lệ lỗi).
  - **D**uration (Độ trễ/Latency).
- **USE Method (Dành cho Resources - Hạ tầng):**
  - **U**tilization (Phần trăm tài nguyên đang được sử dụng).
  - **S**aturation (Mức độ bão hòa, hàng đợi - mức độ quá tải).
  - **E**rrors (Số lượng lỗi phần cứng/hệ thống).
- **Pull vs Push model:** Hiểu tại sao Prometheus sử dụng Pull model (kéo data định kỳ) thay vì Push, và khi nào thì nên dùng Pushgateway.
- **Labels / Tags:** Cách thiết kế cardinality hiệu quả. Lưu ý: Không dùng các giá trị có tính biến thiên cao (như UserID, RequestID) làm label để tránh làm bùng nổ dữ liệu (high cardinality).
- **PromQL (Prometheus Query Language):** Học cách query cơ bản, sử dụng hàm `rate()`, `irate()`, và cách tính percentile `histogram_quantile(0.95, ...)` để tìm ra p95, p99.

## 2. Các công cụ cần Setup
- **Prometheus:** Time-series database để thu thập (scrape) và lưu trữ dữ liệu metrics.
- **Grafana:** Dashboard dùng để hiển thị biểu đồ trực quan và cấu hình Alerting (cảnh báo qua Slack/Telegram/Email).
- **Node Exporter:** Cài đặt trên các server/node để lấy thông số của Hệ điều hành (CPU, RAM, Disk, Network).
- **cAdvisor / Kube-state-metrics:** Thu thập thông số của Container và Kubernetes (nếu hệ thống chạy trên Docker/K8s).
- **Application Metrics Exporter:** Tích hợp thư viện vào code Go (ví dụ: `prometheus/client_golang` hoặc sử dụng OpenTelemetry) để tự động đẩy metrics của ứng dụng ra một endpoint (thường là `/metrics`).

## 3. Những Metric chủ yếu cần theo dõi

### 3.1. Infrastructure & OS (System Metrics)
- **CPU:** Usage (%), Load Average (1m, 5m, 15m).
- **Memory (RAM):** Available, Used, Cached, Buffers.
- **Disk:** Disk Space Usage (%), Disk I/O (Read/Write OPS và Wait time).
- **Network:** Bandwidth In/Out (Rx/Tx), Dropped packets, Errors.

### 3.2. Container / Pod Metrics
- Mức sử dụng CPU/Memory hiện tại so với cấu hình Limit và Request.
- **Container Restarts:** Số lần bị restart (đây là một chỉ báo quan trọng của sự cố).
- Container Network & Block I/O.

### 3.3. Application Metrics (RED Method)
- **HTTP/gRPC Requests:** Tổng số request vào (chia theo endpoint, method, status code).
- **Error Rate:** Tỉ lệ số lượng request trả về lỗi (HTTP 5xx, 4xx) trên tổng số request.
- **Latency (Duration):** Thời gian phản hồi của API (quan tâm nhiều nhất đến p50, p90, p95, p99).
- **Business/Custom Metrics:** Tùy logic nghiệp vụ (Ví dụ: Số lượng user đăng ký mới, số đơn hàng đang xử lý, số lượng item trong queue).

### 3.4. Database / Cache Metrics
- **PostgreSQL/MySQL:** Active/Idle connections, Query latency, Cache hit ratio, Deadlocks.
- **Redis:** Memory used, Evictions, Cache hit/miss ratio, Connected clients.

## 4. Các case Debug thực tế bằng Metric

### 4.1. Bắt bệnh Out Of Memory (OOM)
- **Dấu hiệu:** Service thỉnh thoảng tự restart, thỉnh thoảng request bị rớt mà không rõ nguyên nhân.
- **Metric nhận diện:**
  - **Memory Usage** của container tăng theo một đường chéo liên tục và chạm mức limit (dấu hiệu của Memory Leak).
  - Biểu đồ **Container Restarts** tăng lên mỗi khi memory chạm đỉnh và rớt xuống.
  - OOMKilled counter (nếu trong K8s).
- **Hành động:** Kiểm tra lại code xem có lưu trữ dữ liệu vào biến global mà không giải phóng, hoặc leak goroutine không. Kết hợp lấy pprof heap dump để xem phần nào chiếm RAM.

### 4.2. Bùng nổ Goroutine (Goroutine Leaks trong Go)
- **Dấu hiệu:** Service ăn nhiều RAM, chạy chậm dần theo thời gian.
- **Metric nhận diện:**
  - `go_goroutines` (metric mặc định của prometheus client) tăng tuyến tính lên hàng nghìn hoặc hàng triệu mà không có xu hướng giảm.
- **Hành động:** Sử dụng `pprof` để trace xem goroutine nào đang bị block. Nguyên nhân thường là do quên đóng `channel`, quên timeout khi gọi API bên ngoài, hoặc block do I/O.

### 4.3. CPU Spikes (CPU tăng vọt) / Throttling
- **Dấu hiệu:** API phản hồi chậm đột biến, timeout xuất hiện diện rộng.
- **Metric nhận diện:**
  - **CPU Usage** tiệm cận 100% giới hạn của host hoặc chạm Limit của container.
  - **CPU Throttling metrics** tăng vọt (container bị ép xung lùi lại vì xài quá limit).
- **Hành động:** Xác định xem API nào đang nhận traffic cao bất thường (DDoS, spike traffic), hoặc kiểm tra xem có vòng lặp vô hạn hay logic xử lý mảng quá lớn (ví dụ: json serialize/deserialize payload quá to).

### 4.4. High Latency (Phản hồi chậm dây chuyền)
- **Dấu hiệu:** Biểu đồ duration/latency nhảy cao ở p95, p99.
- **Metric nhận diện:**
  - p95, p99 của API Endpoint A tăng vọt.
  - Đối chiếu với metric của Database: Nếu Query Latency của DB cũng tăng, suy ra lỗi là do DB (ví dụ: thiếu index, table lock).
  - Đối chiếu với metric của các service phụ thuộc: Nếu Service A gọi Service B, và latency của B cũng tăng, lỗi nằm ở B.
- **Hành động:** Sử dụng Distributed Tracing (Jaeger/Tempo) kết hợp để trace chính xác đoạn nào chậm. Check lại Slow Query Log trên DB.

### 4.5. Cạn kiệt Connection Pool (Connection Exhaustion)
- **Dấu hiệu:** Ứng dụng bắn log lỗi không thể connect database, timeout khi tạo query.
- **Metric nhận diện:**
  - `go_sql_stats_open_connections` tiệm cận mức cấu hình `MaxOpenConnections`.
  - `go_sql_stats_in_use_connections` bằng với tổng số open connections (tất cả kết nối đều đang bận).
  - `go_sql_stats_wait_count` tăng liên tục (chỉ báo có request đang phải chờ connection trống).
- **Hành động:** Kiểm tra xem có chỗ nào thực hiện query xong quên `rows.Close()` không, hoặc transaction giữ lâu chưa được commit/rollback. Nếu traffic thực sự tăng, cân nhắc tăng MaxOpenConnections.
