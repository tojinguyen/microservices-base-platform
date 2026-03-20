package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

type rabbitMQ struct {
	conn    *amqp.Connection
	channel *amqp.Channel
}

func NewRabbitMQ(cfg Config) (Broker, error) {
	url := fmt.Sprintf("amqp://%s:%s@%s:%d/", cfg.User, cfg.Password, cfg.Host, cfg.Port)
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}

	return &rabbitMQ{
		conn:    conn,
		channel: ch,
	}, nil
}

func (r *rabbitMQ) Publish(ctx context.Context, topic string, body interface{}) error {
	// 1.Declare Exchange (if not exists)
	err := r.channel.ExchangeDeclare(
		topic,    // name
		"fanout", // type (fanout: send to all queues bound to this exchange)
		true,     // durable
		false,    // auto-deleted
		false,    // internal
		false,    // no-wait
		nil,      // arguments
	)
	if err != nil {
		return err
	}

	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	// 2. Publish message to the exchange
	return r.channel.PublishWithContext(ctx,
		topic, // exchange
		"",    // routing key
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        data,
		},
	)
}

func (r *rabbitMQ) Subscribe(ctx context.Context, topic string, handler Handler) error {
	// 1. Declare Exchange
	err := r.channel.ExchangeDeclare(topic, "fanout", true, false, false, false, nil)
	if err != nil {
		return err
	}

	// 2. Declare a temporary Queue for this service
	q, err := r.channel.QueueDeclare("", false, false, true, false, nil)
	if err != nil {
		return err
	}

	// 3. Bind Queue to Exchange
	err = r.channel.QueueBind(q.Name, "", topic, false, nil)
	if err != nil {
		return err
	}

	// 4. Start consuming messages
	msgs, err := r.channel.Consume(q.Name, "", true, false, false, false, nil)
	if err != nil {
		return err
	}

	go func() {
		for d := range msgs {
			if err := handler(ctx, d.Body); err != nil {
				log.Printf("Error handling message: %v", err)
			}
		}
	}()

	return nil
}

func (r *rabbitMQ) Close() error {
	r.channel.Close()
	return r.conn.Close()
}
