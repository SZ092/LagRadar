package main

import (
	"LagRadar/internal/collector"
	"context"
	"encoding/json"
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
		CheckInterval    string  `yaml:"check_interval"`
		WindowSize       int     `yaml:"window_size"`
		MinWindowSize    int     `yaml:"min_window_size"`
		StalledThreshold float64 `yaml:"stalled_threshold"`
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

	// Create sliding window configuration
	collectorConfig := collector.SlidingWindowConfig{
		WindowSize:       config.Collector.WindowSize,
		MinWindowSize:    config.Collector.MinWindowSize,
		StalledThreshold: config.Collector.StalledThreshold,
		CheckInterval:    checkInterval,
	}

	// Create collector
	brokers := joinBrokers(config.Kafka.Brokers)
	c, err := collector.NewWithSlidingWindow(brokers, collectorConfig)
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
	go c.StartPeriodicCollection(ctx, checkInterval)

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

	// Set defaults if not specified
	if config.HTTP.Address == "" {
		config.HTTP.Address = ":8080"
	}
	if config.HTTP.MetricsPath == "" {
		config.HTTP.MetricsPath = "/metrics"
	}
	if config.Collector.CheckInterval == "" {
		config.Collector.CheckInterval = "60s"
	}
	if config.Collector.WindowSize == 0 {
		config.Collector.WindowSize = 10
	}
	if config.Collector.MinWindowSize == 0 {
		config.Collector.MinWindowSize = 3
	}
	if config.Collector.StalledThreshold == 0 {
		config.Collector.StalledThreshold = 0.1
	}

	return &config, nil
}

func startHTTPServer(config *Config, c *collector.Collector) *http.Server {
	mux := http.NewServeMux()

	// Prometheus metrics endpoint
	mux.Handle(config.HTTP.MetricsPath, promhttp.Handler())

	// Health check endpoint
	mux.HandleFunc("/health", healthHandler)

	// API endpoints
	mux.HandleFunc("/api/v1/status", statusHandler(c))
	mux.HandleFunc("/api/v1/status/", groupStatusHandler(c))

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

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}

func statusHandler(c *collector.Collector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		statuses := c.GetAllGroupStatuses()

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(statuses); err != nil {
			log.Printf("Error encoding response: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	}
}

func groupStatusHandler(c *collector.Collector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		groupID := r.URL.Path[len("/api/v1/status/"):]
		if groupID == "" {
			http.Error(w, "Group ID required", http.StatusBadRequest)
			return
		}

		status, exists := c.GetGroupStatus(groupID)
		if !exists {
			http.Error(w, fmt.Sprintf("Consumer group %s not found", groupID), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(status); err != nil {
			log.Printf("Error encoding response: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	}
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
