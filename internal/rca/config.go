package rca

import "time"

// PublisherConfig holds the configuration for RCA event publisher
type PublisherConfig struct {
	Enabled       bool          `yaml:"enabled"`
	RedisAddr     string        `yaml:"redis_addr"`
	RedisPassword string        `yaml:"redis_password"`
	RedisDB       int           `yaml:"redis_db"`
	StreamKey     string        `yaml:"stream_key"`
	MaxRetries    int           `yaml:"max_retries"`
	RetryInterval string        `yaml:"retry_interval"`
	DeDupeWindow  time.Duration `yaml:"de_dupe_window"`
}
