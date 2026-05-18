package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"backend/pkg/trace"

	amqp "github.com/rabbitmq/amqp091-go"
)

type subscription struct {
	queueName   string
	exchange    string
	routingKey  string
	handler     Handler
	opts        QueueOptions
	isBroadcast bool
}

type rabbitMQ struct {
	cfg       Config
	mu        sync.RWMutex
	conn      *amqp.Connection
	closed    bool
	subs      []subscription
	closeChan chan *amqp.Error
}

func NewRabbitMQ(cfg Config) (Broker, error) {
	r := &rabbitMQ{
		cfg: cfg,
	}

	if err := r.connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	go r.handleReconnect()

	return r, nil
}

func (r *rabbitMQ) connect() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	url := fmt.Sprintf("amqp://%s:%s@%s:%d/", r.cfg.User, r.cfg.Password, r.cfg.Host, r.cfg.Port)
	conn, err := amqp.Dial(url)
	if err != nil {
		return err
	}

	r.conn = conn
	r.closeChan = make(chan *amqp.Error, 1)
	r.conn.NotifyClose(r.closeChan)

	return nil
}

func (r *rabbitMQ) handleReconnect() {
	err, ok := <-r.closeChan
	if !ok {
		r.mu.RLock()
		closed := r.closed
		r.mu.RUnlock()
		if closed {
			return
		}
	}

	log.Printf("RabbitMQ connection closed: %v. Initiating reconnect...", err)
	r.reestablish()
}

func (r *rabbitMQ) reestablish() {
	backoff := 2 * time.Second
	maxBackoff := 60 * time.Second

	for {
		r.mu.RLock()
		if r.closed {
			r.mu.RUnlock()
			return
		}
		r.mu.RUnlock()

		log.Printf("Attempting to reconnect to RabbitMQ in %s...", backoff)
		time.Sleep(backoff)

		if err := r.connect(); err != nil {
			log.Printf("Failed to reconnect to RabbitMQ: %v", err)
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		log.Println("Successfully re-connected to RabbitMQ. Re-establishing subscriptions...")

		r.mu.RLock()
		subs := make([]subscription, len(r.subs))
		copy(subs, r.subs)
		r.mu.RUnlock()

		for _, sub := range subs {
			var err error
			if sub.isBroadcast {
				err = r.subscribeBroadcast(context.Background(), sub.exchange, sub.handler)
			} else {
				err = r.subscribeQueue(context.Background(), sub.queueName, sub.exchange, sub.routingKey, sub.handler, sub.opts)
			}
			if err != nil {
				log.Printf("Failed to restore subscription for exchange=%s queue=%s: %v", sub.exchange, sub.queueName, err)
			}
		}

		go r.handleReconnect()
		return
	}
}

func (r *rabbitMQ) Publish(ctx context.Context, exchange, routingKey string, body interface{}) error {
	r.mu.RLock()
	conn := r.conn
	r.mu.RUnlock()

	if conn == nil || conn.IsClosed() {
		return errors.New("broker: connection is not open")
	}

	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Inject trace context vào AMQP headers để truyền qua RabbitMQ broker
	headers := make(amqp.Table)
	trace.InjectAMQP(ctx, headers)

	err = ch.PublishWithContext(ctx,
		exchange,   // Exchange name
		routingKey, // Routing key (Empty if using Fanout)
		false,      // mandatory
		false,      // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent, // Messages will be persisted to disk
			Headers:      headers,
			Body:         data,
		},
	)

	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}
	return nil
}

func (r *rabbitMQ) QueueSubscribe(ctx context.Context, queueName, exchange, routingKey string, handler Handler) error {
	return r.QueueSubscribeWithOptions(ctx, queueName, exchange, routingKey, handler, QueueOptions{Prefetch: 1})
}

func (r *rabbitMQ) QueueSubscribeWithOptions(ctx context.Context, queueName, exchange, routingKey string, handler Handler, opts QueueOptions) error {
	r.mu.Lock()
	r.subs = append(r.subs, subscription{
		queueName:   queueName,
		exchange:    exchange,
		routingKey:  routingKey,
		handler:     handler,
		opts:        opts,
		isBroadcast: false,
	})
	r.mu.Unlock()

	return r.subscribeQueue(ctx, queueName, exchange, routingKey, handler, opts)
}

func (r *rabbitMQ) QueueDeclare(ctx context.Context, queueName, exchange, routingKey string, opts QueueOptions) error {
	r.mu.RLock()
	conn := r.conn
	r.mu.RUnlock()

	if conn == nil || conn.IsClosed() {
		return errors.New("broker: connection is not open")
	}

	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	_, err = r.declareQueueAndExchange(ch, queueName, exchange, routingKey, opts)
	return err
}

func (r *rabbitMQ) subscribeQueue(ctx context.Context, queueName, exchange, routingKey string, handler Handler, opts QueueOptions) error {
	r.mu.RLock()
	conn := r.conn
	r.mu.RUnlock()

	if conn == nil || conn.IsClosed() {
		return errors.New("broker: connection is not open")
	}

	ch, err := conn.Channel()
	if err != nil {
		return err
	}

	q, err := r.declareQueueAndExchange(ch, queueName, exchange, routingKey, opts)
	if err != nil {
		ch.Close()
		return err
	}

	prefetch := opts.Prefetch
	if prefetch <= 0 {
		prefetch = 1
	}
	err = ch.Qos(prefetch, 0, false)
	if err != nil {
		ch.Close()
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
		ch.Close()
		return err
	}

	go r.handleMessages(ctx, ch, msgs, queueName, handler)

	return nil
}

func (r *rabbitMQ) declareQueueAndExchange(ch *amqp.Channel, queueName, exchange, routingKey string, opts QueueOptions) (amqp.Queue, error) {
	var queueArgs amqp.Table
	if len(opts.QueueArgs) > 0 {
		queueArgs = make(amqp.Table, len(opts.QueueArgs))
		for k, v := range opts.QueueArgs {
			queueArgs[k] = v
		}
	}

	q, err := ch.QueueDeclare(
		queueName,
		true,      // durable
		false,     // auto-delete
		false,     // exclusive
		false,     // no-wait
		queueArgs, // x-max-length etc.
	)
	if err != nil {
		return amqp.Queue{}, err
	}

	if exchange != "" {
		err = ch.ExchangeDeclare(
			exchange,
			"direct",
			true,  // durable
			false, // auto-deleted
			false, // internal
			false, // no-wait
			nil,
		)
		if err != nil {
			return amqp.Queue{}, fmt.Errorf("failed to declare exchange: %w", err)
		}

		err = ch.QueueBind(
			q.Name,
			routingKey,
			exchange,
			false,
			nil,
		)
		if err != nil {
			return amqp.Queue{}, fmt.Errorf("failed to bind queue to exchange: %w", err)
		}
	}

	return q, nil
}

func (r *rabbitMQ) BroadcastSubscribe(ctx context.Context, exchangeName string, handler Handler) error {
	r.mu.Lock()
	r.subs = append(r.subs, subscription{
		exchange:    exchangeName,
		handler:     handler,
		isBroadcast: true,
	})
	r.mu.Unlock()

	return r.subscribeBroadcast(ctx, exchangeName, handler)
}

func (r *rabbitMQ) subscribeBroadcast(ctx context.Context, exchangeName string, handler Handler) error {
	r.mu.RLock()
	conn := r.conn
	r.mu.RUnlock()

	if conn == nil || conn.IsClosed() {
		return errors.New("broker: connection is not open")
	}

	ch, err := conn.Channel()
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

			// Extract trace context từ AMQP headers để tiếp tục chuỗi trace
			msgCtx := trace.ExtractAMQP(ctx, d.Headers)
			err := handler(msgCtx, d.Body)

			if err != nil {
				if errors.Is(err, ErrRejectToDLQ) {
					log.Printf("Error processing message, rejecting to DLQ... Error: %v", err)
					d.Nack(false, false) // requeue = false -> RabbitMQ moves to DLQ
				} else {
					log.Printf("Error processing message, requeueing... Error: %v", err)
					d.Nack(false, true) // requeue = true
				}
			} else {
				d.Ack(false)
			}
		}
	}
}

func (r *rabbitMQ) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.closed = true
	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}
