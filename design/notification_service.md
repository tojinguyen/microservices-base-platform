### NOTIFICATION SERVICE (Dịch vụ Thông báo)

**📝 Mô tả:**

Dịch vụ chịu trách nhiệm gửi thông báo đến người dùng qua nhiều kênh như Email, Push Notification hoặc In-app. Giúp hệ thống giao tiếp với user một cách bất đồng bộ và không làm chậm luồng xử lý chính.

**🧠 Kỹ năng học được:**

Event-driven Architecture, Async Processing, Idempotency, Retry Strategy, Template Engine, Distributed Systems.

**🛠 Đặt ở đâu:**

Hoạt động như một **Microservice độc lập**, nhận event từ **Message Broker (Kafka / RabbitMQ)** và xử lý bất đồng bộ qua Worker.

---

- **🟢 Level Cơ bản (Basic):**
    - Thiết kế DB lưu Notification (user_id, type, content, status).
    - Nhận event từ các service khác (OrderCreated, PaymentSuccess).
    - Gửi Email trực tiếp (sync hoặc async đơn giản).
    - Lưu trạng thái gửi (pending → sent / failed).
    - Hỗ trợ một loại notification cơ bản (Email).

---

- **🔴 Level Nâng cao (Advanced):**
    - **Event-driven Processing:**
    Nhận event từ Kafka / RabbitMQ thay vì gọi HTTP trực tiếp, giúp hệ thống decouple và scale tốt hơn.
    - **Queue-based Worker (Async):**
    Đẩy job gửi notification vào Redis Queue (Asynq / BullMQ) để worker xử lý riêng, tránh block request.
    - **Retry + Exponential Backoff:**
    Khi gửi notification thất bại (SMTP lỗi, push service downtime), retry với chiến lược:*(2s → 4s → 8s → 16s + jitter)*.
    - **Dead Letter Queue (DLQ):**
    Nếu retry quá số lần cho phép, chuyển job sang DLQ để debug và xử lý thủ công.
    - **Idempotency Handling:**
    Lưu `event_id` hoặc `notification_key` để đảm bảo 1 event chỉ gửi notification đúng 1 lần, tránh spam khi message bị duplicate.
    - **Multi-channel Notification:**
    Thiết kế theo Strategy Pattern:
        - EmailSender
        - PushSender
        - InAppSender
    - **User Preferences:**
    Cho phép user bật/tắt từng loại notification (email, push, in-app).
    - **Template Engine:**
    Dùng template động:`"Hello {{name}}, order {{order_id}} confirmed"`
    → dễ maintain và đa ngôn ngữ.
    - **Batch Processing & Scaling:**
    Gom nhiều notification gửi theo batch, scale worker theo tải (horizontal scaling).

---

- **⚠️ Cạm bẫy thực tế (Gotchas):**
    - Gửi notification trực tiếp trong HTTP request → làm chậm hệ thống.
    → **Giải pháp:** luôn xử lý async qua queue.
    - Không xử lý idempotency → user nhận nhiều email trùng.
    → **Giải pháp:** lưu `event_id` và check trước khi xử lý.
    - Retry liên tục khi service downstream chết → gây quá tải hệ thống.
    → **Giải pháp:** dùng exponential backoff + DLQ.
    - Không giới hạn tần suất gửi → spam user.
    → **Giải pháp:** rate limit theo user (ví dụ: max 10 notifications/phút).
    - Không có observability → không biết notification fail ở đâu.
    → **Giải pháp:** logging + tracing + metrics (Prometheus).