package config

import (
	"strings"

	"github.com/spf13/viper"
)

type Options struct {
	ConfigName string
	ConfigType string
	Paths      []string
	EnvPrefix  string
}

func DefaultOptions() Options {
	return Options{
		ConfigName: ".env",
		ConfigType: "env",
		Paths:      []string{".", "./cmd"},
	}
}

func Load(cfg interface{}, opts ...Options) error {
	v := viper.New()

	opt := DefaultOptions()
	if len(opts) > 0 {
		opt = opts[0]
	}

	v.SetConfigName(strings.TrimSuffix(opt.ConfigName, "."+opt.ConfigType))
	v.SetConfigType(opt.ConfigType)
	for _, path := range opt.Paths {
		v.AddConfigPath(path)
	}

	if opt.EnvPrefix != "" {
		v.SetEnvPrefix(opt.EnvPrefix)
	}
	v.AutomaticEnv()

	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	_ = v.ReadInConfig()

	return v.Unmarshal(cfg)
}
