package worker

import (
	"context"

	"backend/pkg/broker"
	"backend/pkg/logger"

	"github.com/tojinguyen/transcoder/internal/config"
	"go.uber.org/zap"
)

type TranscoderWorker struct {
	broker    broker.Broker
	processor *JobProcessor
	cfg       *config.Config
}

func NewTranscoderWorker(b broker.Broker, processor *JobProcessor, cfg *config.Config) *TranscoderWorker {
	return &TranscoderWorker{broker: b, processor: processor, cfg: cfg}
}

func (w *TranscoderWorker) Start(ctx context.Context) error {
	log := logger.L()

	queueName := w.cfg.Transcoder.QueueName
	exchange := w.cfg.Transcoder.SourceExchange
	routingKey := w.cfg.Transcoder.SourceRoutingKey

	prefetch := w.cfg.Transcoder.Concurrency
	if prefetch <= 0 {
		prefetch = 2
	}

	opts := broker.QueueOptions{
		Prefetch: prefetch,
		QueueArgs: map[string]interface{}{
			"x-dead-letter-exchange":    exchange + ".dlq",
			"x-dead-letter-routing-key": queueName + ".failed",
		},
	}

	log.Info("transcoder worker subscribing",
		zap.String("queue", queueName),
		zap.String("exchange", exchange),
		zap.String("routing_key", routingKey),
		zap.Int("prefetch", prefetch),
	)

	return w.broker.QueueSubscribeWithOptions(ctx, queueName, exchange, routingKey, w.handle, opts)
}

func (w *TranscoderWorker) handle(ctx context.Context, body []byte) error {
	return w.processor.Process(ctx, body)
}
