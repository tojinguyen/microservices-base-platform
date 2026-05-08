package worker

import (
	"context"
	"time"

	"backend/pkg/logger"

	"github.com/tojinguyen/notification/internal/service"
	"go.uber.org/zap"
)

const schedulerInterval = 60 * time.Second

type SchedulerWorker interface {
	Start(ctx context.Context)
}

type schedulerWorker struct {
	svc service.SchedulerService
}

func NewSchedulerWorker(svc service.SchedulerService) SchedulerWorker {
	return &schedulerWorker{svc: svc}
}

func (w *schedulerWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(schedulerInterval)
	defer ticker.Stop()

	log := logger.L()
	log.Info("Scheduler worker started", zap.Duration("interval", schedulerInterval))

	// Run immediately on start so we don't wait a full minute on deploy.
	w.run(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Info("Scheduler worker stopping")
			return
		case <-ticker.C:
			w.run(ctx)
		}
	}
}

func (w *schedulerWorker) run(ctx context.Context) {
	if err := w.svc.ProcessDueSchedules(ctx); err != nil {
		logger.L().Error("scheduler worker failed to process due schedules", zap.Error(err))
	}
}
