package config

import (
	"backend/pkg/broker"
	"backend/pkg/db"
	"backend/pkg/redis"
)

const (
	ModeAPI           = "api"
	ModeWorkerJanitor = "worker-janitor"
)

type Config struct {
	AppMode    string          `mapstructure:"app_mode"`
	TimeGrace  int             `mapstructure:"time_grace"`
	ServerPort int             `mapstructure:"port"`
	Database   db.Config       `mapstructure:"database"`
	Redis      redis.Config    `mapstructure:"redis"`
	Broker     broker.Config   `mapstructure:"broker"`
	Storage    StorageConfig   `mapstructure:"storage"`
	Upload     UploadConfig    `mapstructure:"upload"`
	Janitor    JanitorConfig   `mapstructure:"janitor"`
	RateLimit  RateLimitConfig `mapstructure:"rate_limit"`
	Otel       OtelConfig      `mapstructure:"otel"`
}

type StorageConfig struct {
	Endpoint     string `mapstructure:"endpoint"`
	Region       string `mapstructure:"region"`
	Bucket       string `mapstructure:"bucket"`
	AccessKey    string `mapstructure:"access_key"`
	SecretKey    string `mapstructure:"secret_key"`
	UsePathStyle bool   `mapstructure:"use_path_style"`
	PublicURL    string `mapstructure:"public_url"`
}

type UploadConfig struct {
	MaxSizeBytes          int64  `mapstructure:"max_size_bytes"`
	PresignTTLSeconds     int    `mapstructure:"presign_ttl_seconds"`
	AllowedMimeTypes      string `mapstructure:"allowed_mime_types"`
	ChunkSizeMB           int    `mapstructure:"chunk_size_mb"`
	PartPresignTTLSeconds int    `mapstructure:"part_presign_ttl_seconds"`
	SessionTTLHours       int    `mapstructure:"session_ttl_hours"`
}

type JanitorConfig struct {
	IntervalSeconds int `mapstructure:"interval_seconds"`
	BatchSize       int `mapstructure:"batch_size"`
}

type RateLimitConfig struct {
	Limit      int `mapstructure:"limit"`
	WindowSecs int `mapstructure:"window_secs"`
}

type OtelConfig struct {
	Enabled          bool   `mapstructure:"enabled"`
	ExporterEndpoint string `mapstructure:"exporter_otlp_endpoint"`
}
