package config

import (
	"backend/pkg/broker"
	"backend/pkg/db"
	"backend/pkg/redis"
	"time"
)

const (
	ModeAPI              = "api"
	ModeWorkerPending    = "worker-pending"
	ModeWorkerEmail      = "worker-email"
	ModeWorkerWebhook    = "worker-webhook"
	ModeWorkerScheduler  = "worker-scheduler"
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
}

type RateLimitConfig struct {
	GlobalLimit      int `mapstructure:"global_limit"`
	GlobalWindowSecs int `mapstructure:"global_window_secs"`
	SendLimit        int `mapstructure:"send_limit"`
	SendWindowSecs   int `mapstructure:"send_window_secs"`
}
