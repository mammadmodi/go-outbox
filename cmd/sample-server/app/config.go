package app

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"

	"github.com/mammadmodi/go-outbox/outbox"
)

// Config holds the configuration for the outbox relay executable.
type Config struct {
	ServerPort   string             `mapstructure:"server_port"`
	DatabaseDSN  string             `mapstructure:"database_dsn"`
	NatsURL      string             `mapstructure:"nats_url"`
	AdvisoryLock int64              `mapstructure:"advisory_lock"`
	LogLevel     string             `mapstructure:"log_level"`
	LogFormat    string             `mapstructure:"log_format"`
	Relay        outbox.RelayConfig `mapstructure:"relay"`
}

// NewConfig loads configuration from a file and then overrides it with environment variables.
func NewConfig(cfgPath string) (*Config, error) {
	v := viper.New()

	if cfgPath != "" {
		v.SetConfigFile(cfgPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	// Environment variable support
	v.SetEnvPrefix("OUTBOX")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Bind environment variables (explicit)
	_ = v.BindEnv("server_port")
	_ = v.BindEnv("database_dsn")
	_ = v.BindEnv("nats_url")
	_ = v.BindEnv("advisory_lock")
	_ = v.BindEnv("log_level")
	_ = v.BindEnv("log_format")
	_ = v.BindEnv("advisory_lock")
	_ = v.BindEnv("relay.poll_interval_ms")
	_ = v.BindEnv("relay.batch_size")
	_ = v.BindEnv("log_level")

	// Default values
	v.SetDefault("server_port", ":8080")
	v.SetDefault("relay.poll_interval", "1000ms") // 1 second
	v.SetDefault("relay.batch_size", 100)
	v.SetDefault("log_level", "info")
	v.SetDefault("log_format", "text")

	// Unmarshal into struct
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &cfg, nil
}
