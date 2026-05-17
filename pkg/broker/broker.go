package broker

import (
	"context"
	"errors"
)

// ErrQueueFull is returned when the broker rejects a publish because the target queue
// has reached its x-max-length cap. Callers should back off and retry.
var ErrQueueFull = errors.New("broker: queue is full")

// ErrRejectToDLQ is returned when a handler rejects a message explicitly,
// directing the broker to send it to the Dead Letter Queue rather than requeueing it.
var ErrRejectToDLQ = errors.New("broker: reject message to DLQ")


type Config struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
}

type Handler func(ctx context.Context, body []byte) error

// QueueOptions controls advanced queue declaration and consumer behaviour.
type QueueOptions struct {
	// Prefetch is the maximum number of unacknowledged messages the broker will
	// push to this consumer at once (maps to AMQP basic.qos prefetch-count).
	// 0 means "use broker default" (typically unlimited).
	Prefetch int

	// QueueArgs are passed as x-arguments when declaring the queue, e.g.
	// {"x-max-length": 50000, "x-overflow": "reject-publish"}.
	QueueArgs map[string]interface{}
}

type Broker interface {
	Publish(ctx context.Context, exchange, routingKey string, body interface{}) error

	QueueSubscribe(ctx context.Context, queueName, exchange, routingKey string, handler Handler) error

	// QueueSubscribeWithOptions is like QueueSubscribe but accepts QueueOptions
	// so callers can control prefetch count and declare queue arguments (e.g.
	// x-max-length for back-pressure capping).
	QueueSubscribeWithOptions(ctx context.Context, queueName, exchange, routingKey string, handler Handler, opts QueueOptions) error

	BroadcastSubscribe(ctx context.Context, exchangeName string, handler Handler) error

	Close() error
}
