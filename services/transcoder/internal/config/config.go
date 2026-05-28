package config

import (
	"backend/pkg/broker"
	"backend/pkg/db"
)

const (
	ModeWorker = "worker"
)

type Config struct {
	AppMode    string           `mapstructure:"app_mode"`
	TimeGrace  int              `mapstructure:"time_grace"`
	Database   db.Config        `mapstructure:"database"`
	Broker     broker.Config    `mapstructure:"broker"`
	Storage    StorageConfig    `mapstructure:"storage"`
	Transcoder TranscoderConfig `mapstructure:"transcoder"`
	Otel       OtelConfig       `mapstructure:"otel"`
}

type StorageConfig struct {
	Endpoint     string `mapstructure:"endpoint"`
	Region       string `mapstructure:"region"`
	Bucket       string `mapstructure:"bucket"`
	AccessKey    string `mapstructure:"access_key"`
	SecretKey    string `mapstructure:"secret_key"`
	UsePathStyle bool   `mapstructure:"use_path_style"`
}

type TranscoderConfig struct {
	Concurrency        int    `mapstructure:"concurrency"`
	MaxRetries         int    `mapstructure:"max_retries"`
	TmpDir             string `mapstructure:"tmp_dir"`
	HLSPrefix          string `mapstructure:"hls_prefix"`
	SegmentDuration    int    `mapstructure:"segment_duration"`
	UploadConcurrency  int    `mapstructure:"upload_concurrency"`
	QueueName          string `mapstructure:"queue_name"`
	SourceExchange     string `mapstructure:"source_exchange"`
	SourceRoutingKey   string `mapstructure:"source_routing_key"`
	FFmpegBinary       string `mapstructure:"ffmpeg_binary"`
}

type OtelConfig struct {
	Enabled          bool   `mapstructure:"enabled"`
	ExporterEndpoint string `mapstructure:"exporter_otlp_endpoint"`
}
