package service

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tojinguyen/identity/internal/domain"
	"github.com/tojinguyen/identity/internal/dto"
	"github.com/tojinguyen/identity/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

type SeedService interface {
	SeedUsers(ctx context.Context, req *dto.SeedUsersRequest, progressChan chan<- dto.SeedProgressEvent)
}

type seedService struct {
	userRepo repository.UserRepository
}

func NewSeedService(userRepo repository.UserRepository) SeedService {
	return &seedService{userRepo: userRepo}
}

func (s *seedService) SeedUsers(ctx context.Context, req *dto.SeedUsersRequest, progressChan chan<- dto.SeedProgressEvent) {
	defer close(progressChan)

	start := time.Now()

	// 1. Tối ưu: Chỉ hash password 1 lần duy nhất với MinCost (nhanh nhất) 
	// Dùng chung cho tất cả test users để giảm tải CPU
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.MinCost)
	if err != nil {
		progressChan <- dto.SeedProgressEvent{
			Type:    "error",
			Message: fmt.Sprintf("Failed to hash password: %v", err),
		}
		return
	}
	commonHash := string(hashBytes)

	// 2. Setup batching & workers
	batchSize := req.BatchSize
	if batchSize <= 0 {
		batchSize = 5000
	}
	workerCount := req.WorkerCount
	if workerCount <= 0 {
		workerCount = runtime.NumCPU() * 2 // Tận dụng tối đa CPU core cho I/O và Data Gen
	}

	total := req.Count
	var seeded, failed int
	var mu sync.Mutex

	numBatches := (total + batchSize - 1) / batchSize
	jobs := make(chan int, numBatches)
	var wg sync.WaitGroup

	// 3. Khởi tạo Worker Pool
	for w := 0; w < workerCount; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batchIdx := range jobs {
				// Stop nếu request bị hủy (ví dụ client disconnect)
				if ctx.Err() != nil {
					return
				}

				startIdx := batchIdx * batchSize
				endIdx := startIdx + batchSize
				if endIdx > total {
					endIdx = total
				}

				currentBatchSize := endIdx - startIdx
				users := make([]*domain.User, currentBatchSize)

				// Generate data in memory
				for i := 0; i < currentBatchSize; i++ {
					idx := startIdx + i
					users[i] = &domain.User{
						BaseModel: domain.BaseModel{
							Id: uuid.New(),
						},
						Email:        fmt.Sprintf("%s_%d@seed.local", req.Prefix, idx),
						Name:         fmt.Sprintf("Seed User %d", idx),
						PasswordHash: commonHash,
						Role:         "user",
					}
				}

				// Insert batch vào DB
				err := s.userRepo.BulkCreate(ctx, users, batchSize)

				// Update thống kê an toàn với Mutex
				mu.Lock()
				if err != nil {
					failed += currentBatchSize
				} else {
					seeded += currentBatchSize
				}
				currentSeeded := seeded
				currentFailed := failed
				mu.Unlock()

				elapsed := time.Since(start).Milliseconds()
				rate := 0.0
				if elapsed > 0 {
					rate = float64(currentSeeded) / (float64(elapsed) / 1000.0)
				}

				// Emit progress
				select {
				case progressChan <- dto.SeedProgressEvent{
					Type:        "progress",
					Seeded:      currentSeeded,
					Failed:      currentFailed,
					Total:       total,
					ProgressPct: float64(currentSeeded+currentFailed) / float64(total) * 100,
					DurationMs:  elapsed,
					UsersPerSec: rate,
				}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Queue up jobs
	for i := 0; i < numBatches; i++ {
		jobs <- i
	}
	close(jobs)

	// Đợi tất cả workers hoàn thành
	wg.Wait()

	// Check if context was cancelled
	if ctx.Err() != nil {
		progressChan <- dto.SeedProgressEvent{
			Type:    "error",
			Message: "Seed process was cancelled by client",
		}
		return
	}

	// Emit done event
	elapsed := time.Since(start).Milliseconds()
	rate := 0.0
	if elapsed > 0 {
		rate = float64(seeded) / (float64(elapsed) / 1000.0)
	}

	progressChan <- dto.SeedProgressEvent{
		Type:        "done",
		Seeded:      seeded,
		Failed:      failed,
		Total:       total,
		ProgressPct: 100.0,
		DurationMs:  elapsed,
		UsersPerSec: rate,
		Message:     "Seed completed successfully",
	}
}
