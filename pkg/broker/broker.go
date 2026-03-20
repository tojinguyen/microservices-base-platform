package broker

import "context"

type Config struct {
	Host     string
	Port     int
	User     string
	Password string
}

type Handler func(ctx context.Context, body []byte) error

type Broker interface {
	Publish(ctx context.Context, topic string, body interface{}) error
	Subscribe(ctx context.Context, topic string, handler Handler) error
	Close() error
}
