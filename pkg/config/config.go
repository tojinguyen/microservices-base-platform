package config

import (
	"reflect"
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

	bindEnvs(v, cfg)

	return v.Unmarshal(cfg)
}

func bindEnvs(v *viper.Viper, iface interface{}, parts ...string) {
	ifv := reflect.ValueOf(iface)
	if ifv.Kind() == reflect.Ptr {
		ifv = ifv.Elem()
	}

	if ifv.Kind() != reflect.Struct {
		return
	}

	ift := ifv.Type()
	for i := 0; i < ift.NumField(); i++ {
		vField := ifv.Field(i)
		tField := ift.Field(i)

		tv, ok := tField.Tag.Lookup("mapstructure")
		if !ok {
			tv = strings.ToLower(tField.Name)
		}

		if tv == "-" {
			continue
		}

		if vField.Kind() == reflect.Struct {
			bindEnvs(v, vField.Interface(), append(parts, tv)...)
		} else {
			v.BindEnv(strings.Join(append(parts, tv), "."))
		}
	}
}
