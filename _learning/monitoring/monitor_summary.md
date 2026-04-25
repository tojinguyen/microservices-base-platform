# Hướng dẫn cơ bản về Giám sát (Monitoring) trong Kubernetes

Khi chuyển sang môi trường K8s, việc giám sát (monitoring) là cực kỳ quan trọng vì hệ thống phân tán rất phức tạp. Để monitor hiệu quả, chúng ta thường dựa vào **3 trụ cột của Observability (Tính quan sát được)**: **Metrics** (Chỉ số), **Logs** (Nhật ký hệ thống), và **Traces** (Dấu vết request).

Dưới đây là danh sách các công cụ (tools) tiêu chuẩn, tác dụng của chúng và những thông số bạn cần quan tâm:

---

## 1. Các công cụ cần thiết và tác dụng

### A. Nhóm thu thập và hiển thị Metrics (Chỉ số)
*   **Prometheus:**
    *   **Tác dụng:** Là "tiêu chuẩn vàng" trong K8s. Nó tự động tìm kiếm (service discovery) và kéo (pull) các thông số CPU, RAM, Network... từ các Pods, Nodes và lưu trữ dưới dạng time-series database. 
*   **Grafana:**
    *   **Tác dụng:** Đi cặp với Prometheus. Prometheus thu thập số liệu, còn Grafana giúp bạn vẽ biểu đồ, tạo các dashboard trực quan, đẹp mắt và cấu hình cảnh báo (Alert) gửi qua Slack, Telegram hay Email khi có sự cố.
*   **Metrics Server:**
    *   **Tác dụng:** Một tool nhỏ gọn chạy trong K8s, cung cấp số liệu CPU/RAM cơ bản cho K8s để thực hiện các tính năng như HPA (Tự động scale số lượng Pod dựa trên tải).

### B. Nhóm quản lý Logs (Nhật ký)
Trong K8s, các Pod có thể bị tắt và tạo mới liên tục, nếu không lưu log ra bên ngoài, log sẽ bị mất khi Pod chết.
*   **EFK Stack (Elasticsearch, Fluentd/Fluent Bit, Kibana):**
    *   **Fluent Bit:** Thu thập log từ tất cả các Pod/Container.
    *   **Elasticsearch:** Lưu trữ và tìm kiếm log tốc độ cao.
    *   **Kibana:** Giao diện để bạn gõ lệnh tìm kiếm (ví dụ: tìm tất cả log lỗi `500` của `identity-service` trong 15 phút qua).
*   **Loki Stack (Promtail, Loki, Grafana) - Đề xuất cho bạn:**
    *   **Tác dụng:** Tương tự EFK nhưng nhẹ hơn rất nhiều. Nó được thiết kế bởi hãng làm ra Grafana, tích hợp thẳng vào Grafana nên bạn chỉ cần 1 màn hình duy nhất để xem cả Metrics lẫn Logs.

### C. Nhóm Distributed Tracing (Truy vết phân tán)
Khi 1 request đi qua 5-6 microservices, nếu bị chậm, bạn cần biết nó chậm ở service nào.
*   **Jaeger** hoặc **Tempo**:
    *   **Tác dụng:** Ghi nhận lại toàn bộ hành trình của 1 request. Nó sẽ vẽ ra biểu đồ cho thấy request đó tốn 10ms ở API Gateway, 50ms ở Identity Service, và 200ms ở Database. Giúp bạn tìm ra nút thắt cổ chai (bottleneck) cực nhanh.

---

## 2. Cần monitor những thông số gì?

Bạn có thể chia các thông số cần giám sát theo **3 tầng**:

### Tầng 1: Infrastructure & Cluster (Hạ tầng K8s - Node)
Nên áp dụng phương pháp **USE** (Utilization, Saturation, Errors):
*   **CPU / Memory Usage:** Node có bị quá tải không? K8s có cần thêm Node mới không?
*   **Disk Space & Disk I/O:** Ổ cứng còn bao nhiêu %? Tốc độ đọc/ghi có bị nghẽn không (rất quan trọng với Database).
*   **Network In/Out:** Băng thông mạng giữa các Node.
*   **Trạng thái K8s Components:** Kubelet, API Server có đang hoạt động ổn định không.

### Tầng 2: Pods & Containers
*   **Trạng thái Pod:** Bao nhiêu Pod đang chạy (Running), bao nhiêu Pod bị lỗi (CrashLoopBackOff, ImagePullBackOff, Pending, Evicted).
*   **Pod Restarts:** Pod có bị khởi động lại liên tục không? (Thường do lỗi Out of Memory - OOMKilled hoặc lỗi code).
*   **CPU / Memory Quotas:** Pod có đang dùng vượt quá `limits` hoặc `requests` mà bạn đã cấu hình trong file deployment không.

### Tầng 3: Application & Microservices
Nên áp dụng phương pháp **RED** (Rate, Errors, Duration) đối với các APIs:
*   **R - Rate (Tần suất):** Số lượng request gửi đến mỗi service trên 1 giây (RPS - Requests per second).
*   **E - Errors (Lỗi):** Tỉ lệ % request bị lỗi (ví dụ: số lượng HTTP status 5xx, 4xx).
*   **D - Duration (Độ trễ/Thời gian phản hồi):** Service mất bao lâu để trả về kết quả? (thường tính theo p95, p99 - ví dụ 95% request được xử lý dưới 200ms).
*   **Business Metrics (Tùy chọn):** Số lượng user đăng ký mới, số đơn hàng thành công, v.v. (Code của bạn tự xuất ra Prometheus).

---

## Tóm lại: Bạn nên bắt đầu như thế nào?

Đừng cài đặt tất cả mọi thứ cùng lúc vì sẽ rất nặng và ngợp. Lời khuyên cho bạn là làm theo từng bước:

1. **Bước 1 (Bắt buộc):** Cài đặt **Prometheus + Grafana** bằng Helm Chart (kube-prometheus-stack). Nó sẽ tự động có sẵn các dashboard chuẩn nhất để bạn monitor Node và Pod.
2. **Bước 2 (Rất nên làm):** Cài đặt **Loki + Promtail** để tập trung Logs về Grafana. Khi Pod bị lỗi, bạn có thể xem log trực tiếp trên Grafana mà không cần dùng lệnh `kubectl logs ...`.
3. **Bước 3 (Nâng cao):** Cài đặt **Jaeger / Tempo** khi hệ thống microservices của bạn đã lớn và bắt đầu gặp vấn đề khó debug về tốc độ.
