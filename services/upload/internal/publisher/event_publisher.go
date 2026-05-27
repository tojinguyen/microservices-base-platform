package publisher

import (
	"context"

	"backend/pkg/broker"
	"github.com/tojinguyen/upload/internal/dto"
)

const (
	ExchangeVideoEvents    = "video.events"
	RoutingKeyVideoUploaded = "video.uploaded"
)

type EventPublisher struct {
	broker broker.Broker
}

func NewEventPublisher(b broker.Broker) *EventPublisher {
	return &EventPublisher{broker: b}
}

func (p *EventPublisher) PublishVideoUploaded(ctx context.Context, event dto.VideoUploadedEvent) error {
	return p.broker.Publish(ctx, ExchangeVideoEvents, RoutingKeyVideoUploaded, event)
}
