package rca

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"time"
)

// PublisherConfig holds the configuration for RCA event publisher
type PublisherConfig struct {
	Enabled       bool          `yaml:"enabled"`
	StreamKey     string        `yaml:"stream_key"`
	MaxRetries    int           `yaml:"max_retries"`
	RetryInterval string        `yaml:"retry_interval"`
	DeDupeWindow  time.Duration `yaml:"de_dupe_window"`
}

// ConsumerConfig holds consumer configuration
type ConsumerConfig struct {
	ConsumerGroup string        `yaml:"consumer_group"`
	StreamKey     string        `yaml:"stream_key"`
	ConsumerName  string        `yaml:"consumer_name"`
	BatchSize     int64         `yaml:"batch_size"`
	BlockTimeout  time.Duration `yaml:"block_timeout"`
	MaxRetries    int           `yaml:"max_retries"`
}

// Config defines config for RCA service
type Config struct {
	Publisher PublisherConfig `yaml:"publisher"`
	Redis     RedisConfig     `yaml:"redis"`
	Consumer  ConsumerConfig  `yaml:"consumer"`
}

type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// LoadConfig loads configuration from file with environment variable substitution
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Replace environment variables
	configStr := os.ExpandEnv(string(data))

	var config Config
	if err := yaml.Unmarshal([]byte(configStr), &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &config, nil
}
