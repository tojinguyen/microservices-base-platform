# Kiến thức chi tiết về Logging trong Kubernetes

Logging không chỉ là việc in ra màn hình những gì đang xảy ra, mà là một hệ thống giúp bạn "quay ngược thời gian" để tìm hiểu nguyên nhân gốc rễ (Root Cause) của sự cố trong môi trường microservices phức tạp.

---

## 1. Cơ chế Log cơ bản trong K8s

Trong Kubernetes, cơ chế log đơn giản nhất là ghi dữ liệu vào **Standard Output (stdout)** và **Standard Error (stderr)**.

*   **Kubectl Logs:** Khi bạn gõ `kubectl logs <pod_name>`, K8s sẽ đọc dữ liệu từ các file log mà Container Runtime (như Docker hoặc containerd) lưu trên Node.
*   **Vấn đề:** Nếu Pod bị xóa hoặc Node bị lỗi, toàn bộ log này sẽ biến mất. Đó là lý do chúng ta cần một hệ thống quản lý log tập trung (Centralized Logging).

---

## 2. Kiến trúc thu thập Log (Logging Architecture)

Có 3 mô hình chính để thu thập log trong K8s:

### A. Node-level Logging Agent (Khuyên dùng)
*   Một "Agent" (như **Promtail** hoặc **Fluent Bit**) chạy dưới dạng **DaemonSet** trên mỗi Node.
*   Nó sẽ tự động quét thư mục `/var/log/pods` trên Node để gom log của tất cả các Pod và gửi về hệ thống lưu trữ (Loki).
*   **Ưu điểm:** Tiết kiệm tài nguyên, cấu hình một lần cho toàn bộ cluster.

### B. Sidecar Container
*   Bạn chạy một container phụ (Sidecar) nằm chung trong Pod với ứng dụng để đọc file log và đẩy đi.
*   **Sử dụng khi:** Ứng dụng của bạn không ghi log ra stdout mà ghi ra file riêng bên trong container.

### C. Gửi trực tiếp từ ứng dụng
*   Ứng dụng sử dụng SDK để đẩy log trực tiếp về Elasticsearch hoặc Loki.
*   **Nhược điểm:** Làm ứng dụng nặng hơn và khó quản lý nếu có hàng trăm service.

---

## 3. Tìm hiểu về Loki Stack (Loki + Promtail + Grafana)

Loki được mệnh danh là "Prometheus cho Logs" vì nó cực kỳ nhẹ.

*   **Promtail:** Là Agent (người đi thu gom). Nó quét các file log, gán thêm các nhãn (labels) như `namespace`, `pod`, `container` và gửi về Loki.
*   **Loki:** Là nơi lưu trữ (Database). Khác với Elasticsearch, Loki không đánh chỉ mục (index) toàn bộ nội dung log mà chỉ đánh chỉ mục các **Labels**. Điều này giúp Loki tốn rất ít ổ cứng.
*   **Grafana:** Giao diện để bạn truy vấn và vẽ biểu đồ từ log.

---

## 4. Structured Logging (Log có cấu trúc)

Đây là phần quan trọng nhất để bạn có thể debug hiệu quả.

*   **Log thô (Plain Text):** `2023-10-25 10:00:00 INFO User 123 logged in from 1.1.1.1`
    *   => Máy tính rất khó để lọc xem có bao nhiêu user đăng nhập.
*   **Log cấu trúc (JSON):** 
    ```json
    {
      "time": "2023-10-25T10:00:00Z",
      "level": "INFO",
      "msg": "User logged in",
      "user_id": 123,
      "ip": "1.1.1.1",
      "service": "identity-service"
    }
    ```
    *   => Với JSON, Loki và Grafana có thể bóc tách các trường (`user_id`, `ip`) để bạn lọc hoặc đếm lỗi cực nhanh.

---

## 5. Ngôn ngữ truy vấn LogQL

LogQL rất giống với PromQL (của Prometheus). Cấu trúc gồm 2 phần: **Log Stream Selector** và **Log Pipeline**.

*   **Lọc theo nhãn:** `{app="identity-service", env="production"}`
*   **Lọc theo từ khóa:** `{app="notification"} |= "error"` (Tìm log của app notification có chứa chữ "error")
*   **Bóc tách JSON:** `{app="identity"} | json | level="ERROR"` (Bóc tách JSON và chỉ lấy các log có trường level là ERROR)

---

## 6. Các mức độ Log (Log Levels)

Bạn cần sử dụng đúng Log Level để không bị "ngập" trong rác dữ liệu:

1.  **DEBUG:** Thông tin chi tiết để lập trình viên tìm lỗi (Tắt khi chạy Production).
2.  **INFO:** Các sự kiện bình thường (Ví dụ: Service started, User logged in).
3.  **WARN:** Các sự kiện lạ nhưng chưa gây lỗi (Ví dụ: Tốc độ xử lý hơi chậm, thử lại lần 2 mới thành công).
4.  **ERROR:** Có lỗi xảy ra nhưng ứng dụng vẫn chạy tiếp được.
5.  **FATAL/PANIC:** Lỗi nghiêm trọng khiến ứng dụng phải dừng lại ngay lập tức.

---

## 7. Best Practices cho Microservices

1.  **Correlation ID:** Mỗi request vào hệ thống nên được gán 1 mã ID duy nhất. Mã này phải được in ra trong mọi log của tất cả các service mà request đó đi qua. Nhờ đó bạn có thể tìm theo ID này để xem toàn bộ hành trình của request.
2.  **Đừng Log dữ liệu nhạy cảm:** Tuyệt đối không in Password, Credit Card, hoặc Token ra log.
3.  **Log Rotation & Retention:** Thiết lập thời gian xóa log (ví dụ sau 7 hoặc 15 ngày) để tránh làm đầy ổ cứng của server.
4.  **Ghi Log ra stdout:** Luôn ưu tiên ghi log ra stdout để K8s và Promtail dễ dàng thu thập.
