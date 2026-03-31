package config

import (
	"backend/pkg/broker"
	"backend/pkg/db"
)

type Config struct {
	TimeGrace  int           `mapstructure:"time_grace"`
	ServerPort int           `mapstructure:"port"`
	Database   db.Config     `mapstructure:"database"`
	Broker     broker.Config `mapstructure:"broker"`
	SMTP       SMTPConfig    `mapstructure:"smtp"`
	Queue      QueueConfig   `mapstructure:"queue"`
}

type SMTPConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	From     string `mapstructure:"from"`
}

type QueueConfig struct {
	NotificationEvents string `mapstructure:"notification_events"`
}
