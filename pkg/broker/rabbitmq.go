package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

type rabbitMQ struct {
	conn *amqp.Connection
}

func NewRabbitMQ(cfg Config) (Broker, error) {
	url := fmt.Sprintf("amqp://%s:%s@%s:%d/", cfg.User, cfg.Password, cfg.Host, cfg.Port)

	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	return &rabbitMQ{
		conn: conn,
	}, nil
}

func (r *rabbitMQ) Publish(ctx context.Context, exchange, routingKey string, body interface{}) error {
	ch, err := r.conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	err = ch.PublishWithContext(ctx,
		exchange,   // Exchange name
		routingKey, // Routing key (Empty if using Fanout)
		false,      // mandatory
		false,      // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent, // Messages will be persisted to disk
			Body:         data,
		},
	)

	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}
	return nil
}

func (r *rabbitMQ) QueueSubscribe(ctx context.Context, queueName string, handler Handler) error {
	ch, err := r.conn.Channel()
	if err != nil {
		return err
	}

	q, err := ch.QueueDeclare(
		queueName,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		return err
	}

	err = ch.Qos(1, 0, false)
	if err != nil {
		return err
	}

	msgs, err := ch.Consume(
		q.Name,
		"",    // consumer tag
		false, // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		return err
	}

	go r.handleMessages(ctx, ch, msgs, queueName, handler)

	return nil
}

func (r *rabbitMQ) BroadcastSubscribe(ctx context.Context, exchangeName string, handler Handler) error {
	ch, err := r.conn.Channel()
	if err != nil {
		return err
	}

	err = ch.ExchangeDeclare(
		exchangeName,
		"fanout", // type
		true,     // durable
		false,    // auto-deleted
		false,    // internal
		false,    // no-wait
		nil,      // arguments
	)
	if err != nil {
		return err
	}

	q, err := ch.QueueDeclare(
		"",    // Empty name: RabbitMQ will generate a unique name for this temporary queue
		true,  // durable
		false, // auto-delete
		true,  // exclusive
		false, // no-wait
		nil,
	)
	if err != nil {
		return err
	}

	err = ch.QueueBind(q.Name, "", exchangeName, false, nil)
	if err != nil {
		return err
	}

	msgs, err := ch.Consume(
		q.Name,
		"",
		false, // auto-ack
		true,  // exclusive
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	go r.handleMessages(ctx, ch, msgs, exchangeName, handler)

	return nil
}

func (r *rabbitMQ) handleMessages(ctx context.Context, ch *amqp.Channel, msgs <-chan amqp.Delivery, topic string, handler Handler) {
	defer func() {
		log.Printf("Closing channel for topic/queue: %s", topic)
		ch.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			log.Printf("Subscriber stopped by context cancellation: %s", topic)
			return

		case d, ok := <-msgs:
			if !ok {
				log.Printf("Message channel closed: %s", topic)
				return
			}

			err := handler(ctx, d.Body)

			if err != nil {
				log.Printf("Error processing message, requeueing... Error: %v", err)
				d.Nack(false, true)
			} else {
				d.Ack(false)
			}
		}
	}
}

func (r *rabbitMQ) Close() error {
	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}
