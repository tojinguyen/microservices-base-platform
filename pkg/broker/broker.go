package broker

import (
	"context"
)

type Config struct {
	Host     string
	Port     int
	User     string
	Password string
}

type Handler func(ctx context.Context, body []byte) error

type Broker interface {
	Publish(ctx context.Context, exchange, routingKey string, body interface{}) error

	QueueSubscribe(ctx context.Context, queueName string, handler Handler) error

	BroadcastSubscribe(ctx context.Context, exchangeName string, handler Handler) error

	Close() error
}
