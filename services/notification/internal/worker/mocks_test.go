package worker

import (
	"context"
	"os"
	"testing"

	"backend/pkg/broker"
	"backend/pkg/logger"
	"github.com/stretchr/testify/mock"
)

func TestMain(m *testing.M) {
	_ = logger.Init("notification-worker-test", "test")
	os.Exit(m.Run())
}

// mockBroker is a testify mock for broker.Broker used only in worker tests.
type mockBroker struct {
	mock.Mock
}

func (m *mockBroker) Publish(ctx context.Context, exchange, routingKey string, body interface{}) error {
	args := m.Called(ctx, exchange, routingKey, body)
	return args.Error(0)
}

func (m *mockBroker) QueueSubscribe(ctx context.Context, queueName, exchange, routingKey string, handler broker.Handler) error {
	args := m.Called(ctx, queueName, exchange, routingKey, handler)
	return args.Error(0)
}

func (m *mockBroker) QueueSubscribeWithOptions(ctx context.Context, queueName, exchange, routingKey string, handler broker.Handler, opts broker.QueueOptions) error {
	args := m.Called(ctx, queueName, exchange, routingKey, handler, opts)
	return args.Error(0)
}

func (m *mockBroker) BroadcastSubscribe(ctx context.Context, exchangeName string, handler broker.Handler) error {
	args := m.Called(ctx, exchangeName, handler)
	return args.Error(0)
}

func (m *mockBroker) Close() error {
	args := m.Called()
	return args.Error(0)
}
