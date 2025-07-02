package main

import (
	"LagRadar/internal/api"
	"LagRadar/internal/collector"
	"context"
	"fmt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v3"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Config represents the application configuration
type Config struct {
	Kafka struct {
		Brokers []string `yaml:"brokers"`
	} `yaml:"kafka"`

	Collector struct {
		CheckInterval          string        `yaml:"check_interval"`
		WindowSize             int           `yaml:"window_size"`
		MinWindowSize          int           `yaml:"min_window_size"`
		StalledConsumptionRate float64       `yaml:"stalled_threshold"`
		RapidLagIncreaseRate   float64       `yaml:"rapid_lag_increase_threshold"`
		LagTrendThreshold      float64       `yaml:"lag_trend_threshold"`
		InactivityTimeout      time.Duration `yaml:"inactivity_timeout"`
		MaxConcurrency         int           `yaml:"max_concurrency"`
	} `yaml:"collector"`

	HTTP struct {
		Address     string `yaml:"address"`
		MetricsPath string `yaml:"metrics_path"`
	} `yaml:"http"`
}

func main() {

	config, err := loadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("Starting LagRadar...")

	checkInterval, err := time.ParseDuration(config.Collector.CheckInterval)
	if err != nil {
		log.Fatalf("Invalid check interval: %v", err)
	}

	collectorConfig := collector.Config{
		WindowSize:             config.Collector.WindowSize,
		MinWindowSize:          config.Collector.MinWindowSize,
		CheckInterval:          checkInterval,
		StalledConsumptionRate: config.Collector.StalledConsumptionRate,
		RapidLagIncreaseRate:   config.Collector.RapidLagIncreaseRate,
		LagTrendThreshold:      config.Collector.LagTrendThreshold,
		InactivityTimeout:      config.Collector.InactivityTimeout,
		MaxConcurrency:         config.Collector.MaxConcurrency,
	}

	// Create collector
	brokers := joinBrokers(config.Kafka.Brokers)
	c, err := collector.NewWithConfig(brokers, collectorConfig)
	if err != nil {
		log.Fatalf("Failed to create collector: %v", err)
	}
	defer c.Close()

	// Setup context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start HTTP server
	srv := startHTTPServer(config, c)

	// Start periodic collection
	go c.StartPeriodicCollection(ctx)

	// Wait for shutdown signal
	waitForShutdown()

	log.Println("Shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Shutdown complete")
}

func loadConfig(filename string) (*Config, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &config, nil
}

func startHTTPServer(config *Config, c *collector.Collector) *http.Server {
	mux := http.NewServeMux()

	// Prometheus metrics endpoint
	mux.Handle(config.HTTP.MetricsPath, promhttp.Handler())

	// Health check endpoint
	mux.HandleFunc("/health", api.HealthHandler)
	mux.HandleFunc("/ready", api.ReadyHandler(c))

	// API endpoints
	mux.HandleFunc("/api/v1/status", api.StatusHandler(c))
	mux.HandleFunc("/api/v1/status/", api.GroupStatusHandler(c))
	mux.HandleFunc("/api/v1/groups", api.GroupsHandler(c))
	mux.HandleFunc("/api/v1/config", api.ConfigHandler(config))

	srv := &http.Server{
		Addr:    config.HTTP.Address,
		Handler: mux,
	}

	go func() {
		log.Printf("Starting HTTP server on %s", config.HTTP.Address)
		log.Printf("Metrics endpoint: %s", config.HTTP.MetricsPath)

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	return srv
}

func waitForShutdown() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan
	log.Printf("Received signal %v", sig)
}

func joinBrokers(brokers []string) string {
	result := ""
	for i, broker := range brokers {
		if i > 0 {
			result += ","
		}
		result += broker
	}
	return result
}
