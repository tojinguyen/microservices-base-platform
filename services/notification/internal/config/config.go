package config

import (
	"backend/pkg/broker"
	"backend/pkg/db"
)

const (
	ModeAPI           = "api"
	ModeWorkerPending = "worker-pending"
	ModeWorkerEmail   = "worker-email"
)

type Config struct {
	AppMode    string        `mapstructure:"app_mode"`
	TimeGrace  int           `mapstructure:"time_grace"`
	ServerPort int           `mapstructure:"port"`
	Database   db.Config     `mapstructure:"database"`
	Broker     broker.Config `mapstructure:"broker"`
	SMTP       SMTPConfig    `mapstructure:"smtp"`
	Queue      QueueConfig   `mapstructure:"queue"`
	Worker     WorkerConfig  `mapstructure:"worker"`
}

type SMTPConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	From     string `mapstructure:"from"`
}

type QueueConfig struct {
	Exchange string `mapstructure:"exchange"`
}

type WorkerConfig struct {
	BatchSize int `mapstructure:"batch_size"`
	Interval  int `mapstructure:"interval"`
}
