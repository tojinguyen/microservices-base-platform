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
| Metric Name | Mô tả |
|---|---|
| `node_cpu_seconds_total` | Tổng thời gian CPU theo từng mode (idle, user, system, iowait) |
| `node_load1`, `node_load5`, `node_load15` | Load Average của hệ thống theo 1, 5, 15 phút |
| `node_memory_MemAvailable_bytes` | RAM còn trống (quan trọng nhất, khác với MemFree) |
| `node_memory_MemTotal_bytes` | Tổng dung lượng RAM |
| `node_memory_Cached_bytes` | RAM đang được dùng làm cache (có thể giải phóng) |
| `node_filesystem_avail_bytes` | Dung lượng disk trống |
| `node_filesystem_size_bytes` | Tổng dung lượng disk |
| `node_disk_io_time_seconds_total` | Thời gian disk đang bận (I/O Utilization) |
| `node_disk_read_bytes_total` | Tổng số byte đã đọc từ disk |
| `node_disk_written_bytes_total` | Tổng số byte đã ghi vào disk |
| `node_network_receive_bytes_total` | Tổng bandwidth nhận vào (Rx) |
| `node_network_transmit_bytes_total` | Tổng bandwidth gửi đi (Tx) |
| `node_network_receive_drop_total` | Số gói tin bị drop khi nhận |
| `node_network_transmit_drop_total` | Số gói tin bị drop khi gửi |

### 3.2. Container / Pod Metrics (cAdvisor)
| Metric Name | Mô tả |
|---|---|
| `container_cpu_usage_seconds_total` | Tổng CPU time container đã dùng |
| `container_cpu_cfs_throttled_seconds_total` | Tổng thời gian container bị throttle do vượt CPU limit |
| `container_memory_usage_bytes` | RAM container đang dùng (bao gồm cache) |
| `container_memory_working_set_bytes` | RAM "thực sự" container đang dùng (không gồm evictable cache) — **dùng metric này để so với limit** |
| `container_memory_cache` | Phần RAM container dùng làm page cache |
| `container_oom_events_total` | Số lần container bị OOM Kill |
| `container_restarts_total` | Số lần container bị restart |
| `container_network_receive_bytes_total` | Bandwidth nhận vào của container |
| `container_network_transmit_bytes_total` | Bandwidth gửi đi của container |
| `container_fs_reads_bytes_total` | Số byte đọc từ filesystem |
| `container_fs_writes_bytes_total` | Số byte ghi vào filesystem |

### 3.3. Kubernetes Cluster Metrics (kube-state-metrics)
| Metric Name | Mô tả |
|---|---|
| `kube_pod_status_phase` | Trạng thái hiện tại của Pod (Pending, Running, Failed, Succeeded) |
| `kube_pod_container_status_restarts_total` | Số lần restart của container trong Pod |
| `kube_pod_container_status_ready` | Pod container đã sẵn sàng nhận traffic chưa |
| `kube_deployment_status_replicas_available` | Số replica thực sự đang chạy và ready |
| `kube_deployment_spec_replicas` | Số replica mong muốn theo cấu hình |
| `kube_deployment_status_replicas_unavailable` | Số replica đang không khả dụng |
| `kube_node_status_condition` | Trạng thái của từng Node (Ready, MemoryPressure, DiskPressure) |
| `kube_persistentvolumeclaim_status_phase` | Trạng thái của PVC (Bound, Pending, Lost) |
| `kube_job_failed` | Số lần Job bị thất bại |
| `kube_cronjob_next_schedule_time` | Thời điểm CronJob sẽ chạy tiếp theo |

### 3.4. Application / Service Metrics (RED Method)
| Metric Name (ví dụ) | Mô tả |
|---|---|
| `http_requests_total` | Tổng số HTTP request (label: `method`, `path`, `status_code`) |
| `http_request_duration_seconds` | Histogram latency của HTTP request (dùng để tính p50, p95, p99) |
| `http_requests_in_flight` | Số request đang được xử lý tại một thời điểm (concurrency) |
| `grpc_server_started_total` | Tổng số gRPC call bắt đầu |
| `grpc_server_handled_total` | Tổng số gRPC call đã xử lý xong (label: `grpc_code`) |
| `grpc_server_handling_seconds` | Histogram latency của gRPC call |
| `app_business_orders_created_total` | (Custom) Số đơn hàng được tạo thành công |
| `app_business_users_registered_total` | (Custom) Số user đăng ký mới |
| `app_business_active_sessions` | (Custom, Gauge) Số session đang hoạt động |

### 3.5. Database Metrics
| Metric Name | Mô tả |
|---|---|
| `go_sql_stats_open_connections` | Số connection đang mở trong pool |
| `go_sql_stats_in_use_connections` | Số connection đang được dùng để thực hiện query |
| `go_sql_stats_idle_connections` | Số connection đang rảnh trong pool |
| `go_sql_stats_wait_count` | Tổng số lần request phải chờ lấy connection |
| `go_sql_stats_wait_duration_seconds_total` | Tổng thời gian bị chờ connection |
| `pg_stat_bgwriter_buffers_alloc_total` | *(PostgreSQL Exporter)* Số buffer được cấp phát |
| `pg_stat_database_blks_hit` | *(PostgreSQL Exporter)* Số block được đọc từ RAM cache (cache hit) |
| `pg_stat_database_blks_read` | *(PostgreSQL Exporter)* Số block phải đọc từ disk (cache miss) |
| `pg_stat_database_deadlocks` | *(PostgreSQL Exporter)* Số lần xảy ra deadlock |
| `pg_stat_database_numbackends` | *(PostgreSQL Exporter)* Số client đang kết nối |

### 3.6. Cache (Redis) Metrics
| Metric Name | Mô tả |
|---|---|
| `redis_memory_used_bytes` | Lượng RAM Redis đang dùng |
| `redis_memory_max_bytes` | Giới hạn RAM của Redis (maxmemory) |
| `redis_keyspace_hits_total` | Số lần GET key thành công (cache hit) |
| `redis_keyspace_misses_total` | Số lần GET key thất bại (cache miss) |
| `redis_evicted_keys_total` | Số key bị xóa do memory đầy (tăng đột biến là dấu hiệu nguy hiểm) |
| `redis_connected_clients` | Số client đang kết nối vào Redis |
| `redis_commands_processed_total` | Tổng số lệnh Redis đã xử lý |
| `redis_command_duration_seconds` | Latency của các lệnh Redis |
| `redis_expired_keys_total` | Số key đã hết TTL và bị xóa |

### 3.7. Message Queue / Worker Metrics
| Metric Name (ví dụ) | Mô tả |
|---|---|
| `rabbitmq_queue_messages_ready` | Số message đang chờ trong queue chưa được consume |
| `rabbitmq_queue_messages_unacknowledged` | Số message đã deliver cho consumer nhưng chưa được ack |
| `rabbitmq_queue_consumers` | Số consumer đang lắng nghe queue |
| `rabbitmq_channel_messages_published_total` | Tổng số message đã publish vào queue |
| `kafka_consumergroup_lag` | Số message consumer chưa kịp xử lý (consumer lag) |
| `kafka_topic_partition_current_offset` | Offset hiện tại của partition |
| `app_worker_jobs_processed_total` | (Custom) Tổng số job worker đã xử lý xong |
| `app_worker_jobs_failed_total` | (Custom) Tổng số job worker xử lý thất bại |
| `app_worker_job_duration_seconds` | (Custom, Histogram) Thời gian xử lý một job |

### 3.8. Go Runtime Metrics
| Metric Name | Mô tả |
|---|---|
| `go_goroutines` | Số goroutine đang tồn tại (tăng liên tục không giảm = goroutine leak) |
| `go_threads` | Số OS thread đang được tạo |
| `go_gc_duration_seconds` | Histogram thời gian mỗi lần Garbage Collection (GC) |
| `go_memstats_alloc_bytes` | Lượng heap memory đang được cấp phát |
| `go_memstats_heap_inuse_bytes` | Heap đang được dùng bởi object Go |
| `go_memstats_heap_idle_bytes` | Heap đang trống và có thể trả về OS |
| `go_memstats_sys_bytes` | Tổng memory process Go đã lấy từ OS |
| `go_memstats_gc_cpu_fraction` | Phần trăm CPU mà GC đang tiêu tốn (nên < 5%) |
| `go_memstats_next_gc_bytes` | Mốc heap size mà GC sẽ kích hoạt ở lần tiếp theo |

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
