package publisher

import (
	"context"

	"backend/pkg/broker"

	"github.com/tojinguyen/transcoder/internal/dto"
)

const (
	ExchangeVideoEvents             = "video.events"
	RoutingKeyVideoTranscoded       = "video.transcoded"
	RoutingKeyVideoTranscodeFailed  = "video.transcode_failed"
)

type EventPublisher struct {
	broker broker.Broker
}

func NewEventPublisher(b broker.Broker) *EventPublisher {
	return &EventPublisher{broker: b}
}

func (p *EventPublisher) PublishVideoTranscoded(ctx context.Context, evt dto.VideoTranscodedEvent) error {
	return p.broker.Publish(ctx, ExchangeVideoEvents, RoutingKeyVideoTranscoded, evt)
}

func (p *EventPublisher) PublishVideoTranscodeFailed(ctx context.Context, evt dto.VideoTranscodeFailedEvent) error {
	return p.broker.Publish(ctx, ExchangeVideoEvents, RoutingKeyVideoTranscodeFailed, evt)
}
