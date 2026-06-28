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
	ModeWorkerDLQ       = "worker-dlq"
)

type Config struct {
	AppMode          string          `mapstructure:"app_mode"`
	TimeGrace        int             `mapstructure:"time_grace"`
	ServerPort       int             `mapstructure:"port"`
	IdentityGRPCAddr string          `mapstructure:"identity_grpc_addr"`
	Database         db.Config       `mapstructure:"database"`
	Redis            redis.Config    `mapstructure:"redis"`
	Broker           broker.Config   `mapstructure:"broker"`
	SMTP             SMTPConfig      `mapstructure:"smtp"`
	Queue            QueueConfig     `mapstructure:"queue"`
	Worker           WorkerConfig    `mapstructure:"worker"`
	RateLimit        RateLimitConfig `mapstructure:"rate_limit"`
	Otel             OtelConfig      `mapstructure:"otel"`
}

type OtelConfig struct {
	Enabled          bool   `mapstructure:"enabled"`
	ExporterEndpoint string `mapstructure:"exporter_otlp_endpoint"`
}

type SMTPConfig struct {
	Host          string `mapstructure:"host"`
	Port          int    `mapstructure:"port"`
	Username      string `mapstructure:"username"`
	Password      string `mapstructure:"password"`
	From          string `mapstructure:"from"`
	MailpitAPIURL string `mapstructure:"mailpit_api_url"` // URL nội bộ để gọi Mailpit REST API, e.g. http://mailpit-service:8025
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
	CampaignBatchSize          int           `mapstructure:"campaign_batch_size"`
	CampaignPrefetch           int           `mapstructure:"campaign_prefetch"`

	OutboxInterval   int `mapstructure:"outbox_interval"`
	OutboxBatchSize  int `mapstructure:"outbox_batch_size"`
	OutboxMaxRetries int `mapstructure:"outbox_max_retries"`
}

type RateLimitConfig struct {
	GlobalLimit      int `mapstructure:"global_limit"`
	GlobalWindowSecs int `mapstructure:"global_window_secs"`
	SendLimit        int `mapstructure:"send_limit"`
	SendWindowSecs   int `mapstructure:"send_window_secs"`
}
