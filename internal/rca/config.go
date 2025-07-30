package rca

import "time"

// PublisherConfig holds the configuration for RCA event publisher
type PublisherConfig struct {
	Enabled       bool          `yaml:"enabled"`
	StreamKey     string        `yaml:"stream_key"`
	MaxRetries    int           `yaml:"max_retries"`
	RetryInterval string        `yaml:"retry_interval"`
	DeDupeWindow  time.Duration `yaml:"de_dupe_window"`
}

// Config defines config for RCA service
type Config struct {
	Publisher PublisherConfig `yaml:"publisher"`
	Redis     RedisConfig     `yaml:"redis"`
}

type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}
