package config

import (
	"backend/pkg/broker"
	"backend/pkg/db"
	"backend/pkg/redis"
	"time"
)

const (
	ModeAPI             = "api"
	ModeWorkerPending   = "worker-pending"
	ModeWorkerEmail     = "worker-email"
	ModeWorkerWebhook   = "worker-webhook"
	ModeWorkerScheduler = "worker-scheduler"
	ModeWorkerCampaign  = "worker-campaign"
	ModeWorkerOutbox    = "worker-outbox"
)

type Config struct {
	AppMode    string          `mapstructure:"app_mode"`
	TimeGrace  int             `mapstructure:"time_grace"`
	ServerPort int             `mapstructure:"port"`
	Database   db.Config       `mapstructure:"database"`
	Redis      redis.Config    `mapstructure:"redis"`
	Broker     broker.Config   `mapstructure:"broker"`
	SMTP       SMTPConfig      `mapstructure:"smtp"`
	Queue      QueueConfig     `mapstructure:"queue"`
	Worker     WorkerConfig    `mapstructure:"worker"`
	RateLimit  RateLimitConfig `mapstructure:"rate_limit"`
}

type SMTPConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	From     string `mapstructure:"from"`
}

type QueueConfig struct {
	Exchange       string `mapstructure:"exchange"`
	WebhookMailpit string `mapstructure:"webhook_mailpit"`
}

type WorkerConfig struct {
	BatchSize                  int           `mapstructure:"batch_size"`
	Interval                   int           `mapstructure:"interval"`
	MaxRetries                 int           `mapstructure:"max_retries"`
	CircuitBreakerMaxFailures  int           `mapstructure:"circuit_breaker_max_failures"`
	CircuitBreakerOpenDuration time.Duration `mapstructure:"circuit_breaker_open_duration"`
	// Campaign dispatcher reads up to CampaignBatchSize recipients per DB round-trip.
	// Defaults to 1000 if unset.
	CampaignBatchSize int `mapstructure:"campaign_batch_size"`
	// CampaignPrefetch is the RabbitMQ consumer prefetch for the campaign.email queue
	// (back-pressure layer 1). Defaults to 20 if unset.
	CampaignPrefetch int `mapstructure:"campaign_prefetch"`

	// OutboxInterval is the poll interval in seconds for the outbox worker. Defaults to 2.
	OutboxInterval int `mapstructure:"outbox_interval"`
	// OutboxBatchSize is the number of outbox events processed per tick. Defaults to 100.
	OutboxBatchSize int `mapstructure:"outbox_batch_size"`
	// OutboxMaxRetries is the maximum publish attempts before an event is marked failed. Defaults to 5.
	OutboxMaxRetries int `mapstructure:"outbox_max_retries"`
}

type RateLimitConfig struct {
	GlobalLimit      int `mapstructure:"global_limit"`
	GlobalWindowSecs int `mapstructure:"global_window_secs"`
	SendLimit        int `mapstructure:"send_limit"`
	SendWindowSecs   int `mapstructure:"send_window_secs"`
}
