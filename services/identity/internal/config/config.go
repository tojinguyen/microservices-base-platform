package config

import (
	"backend/pkg/auth"
	"backend/pkg/db"
	"backend/pkg/redis"
)

type Config struct {
	TimeGrace   int               `mapstructure:"time_grace"`
	ServerPort  int               `mapstructure:"port"`
	GRPCPort    int               `mapstructure:"grpc_port"`
	Database    db.Config         `mapstructure:"database"`
	JWT         auth.Config       `mapstructure:"jwt"`
	GoogleOAuth GoogleOAuthConfig `mapstructure:"google_oauth"`
	Redis       redis.Config      `mapstructure:"redis"`
}

type GoogleOAuthConfig struct {
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
	RedirectURL  string `mapstructure:"redirect_url"`
}
