package config

import (
	"backend/pkg/auth"
	"backend/pkg/db"
)

type Config struct {
	TimeGrace   int               `mapstructure:"time_grace"`
	ServerPort  int               `mapstructure:"port"`
	Database    db.Config         `mapstructure:"database"`
	JWT         auth.Config       `mapstructure:"jwt"`
	GoogleOAuth GoogleOAuthConfig `mapstructure:"google_oauth"`
}

type GoogleOAuthConfig struct {
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
	RedirectURL  string `mapstructure:"redirect_url"`
}
