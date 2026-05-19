package dto

// SeedUsersRequest là request body cho API seed users.
// Dùng cho mục đích development/testing — KHÔNG dùng trên production.
type SeedUsersRequest struct {
	// Count là tổng số users cần tạo (tối đa 10.000.000)
	Count int `json:"count" binding:"required,min=1,max=10000000"`

	// Prefix là tiền tố email: {prefix}_{i}@seed.local
	// Mặc định: "user"
	Prefix string `json:"prefix"`

	// Password là mật khẩu mặc định cho tất cả seeded users
	// Mặc định: "Seed@1234"
	Password string `json:"password"`

	// BatchSize là số lượng records mỗi lần bulk insert vào DB
	// Mặc định: 5000
	BatchSize int `json:"batch_size"`

	// WorkerCount là số goroutines song song để generate + insert
	// Mặc định: số CPU * 2
	WorkerCount int `json:"worker_count"`
}

// SeedUsersResponse là kết quả sau khi seed hoàn thành.
type SeedUsersResponse struct {
	TotalSeeded   int     `json:"total_seeded"`
	TotalFailed   int     `json:"total_failed"`
	DurationMs    int64   `json:"duration_ms"`
	UsersPerSec   float64 `json:"users_per_sec"`
}

// SeedProgressEvent là event SSE được stream về client trong quá trình seed.
type SeedProgressEvent struct {
	Type        string  `json:"type"`          // "progress" | "done" | "error"
	Seeded      int     `json:"seeded"`        // số users đã insert thành công
	Failed      int     `json:"failed"`        // số users bị lỗi
	Total       int     `json:"total"`         // tổng số cần seed
	ProgressPct float64 `json:"progress_pct"`  // 0.0 → 100.0
	Message     string  `json:"message,omitempty"`
	DurationMs  int64   `json:"duration_ms,omitempty"`
	UsersPerSec float64 `json:"users_per_sec,omitempty"`
}
