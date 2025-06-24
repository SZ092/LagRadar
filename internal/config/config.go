package config

import (
	"flag"
)

type Config struct {
	Brokers        string
	MetricsPort    int
	ScrapeInterval int
	RunOnce        bool
	GroupFilter    string
}

func ParseFlags() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.Brokers, "brokers", "localhost:9092", "Kafka broker addresses")
	flag.IntVar(&cfg.MetricsPort, "port", 9090, "Port to expose metrics on")
	flag.IntVar(&cfg.ScrapeInterval, "interval", 30, "Scrape interval in seconds")
	flag.BoolVar(&cfg.RunOnce, "once", false, "Run once and print to stdout (CLI mode)")
	flag.StringVar(&cfg.GroupFilter, "group", "", "Specific consumer group to monitor (CLI mode only)")

	flag.Parse()

	return cfg
}
