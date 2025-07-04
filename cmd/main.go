package main

import (
	"LagRadar/internal/api"
	"LagRadar/internal/cluster"
	"LagRadar/internal/collector"
	"context"
	"fmt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v3"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Config represents the application configuration
type Config struct {

	// Global collector settings
	Collector struct {
		Window struct {
			Size    int `yaml:"size"`
			MinSize int `yaml:"min_size"`
		} `yaml:"window"`
		CheckInterval string `yaml:"check_interval"`
		Thresholds    struct {
			StalledConsumptionRate float64 `yaml:"stalled_consumption_rate"`
			RapidLagIncreaseRate   float64 `yaml:"rapid_lag_increase_rate"`
			LagTrendThreshold      float64 `yaml:"lag_trend_threshold"`
			InactivityTimeout      string  `yaml:"inactivity_timeout"`
		} `yaml:"thresholds"`
		MaxConcurrency int `yaml:"max_concurrency"`
	} `yaml:"collector"`

	// Legacy single cluster support - backward compatibility
	HTTP struct {
		Address     string `yaml:"address"`
		MetricsPath string `yaml:"metrics_path"`
	} `yaml:"http"`

	Kafka struct {
		Brokers []string `yaml:"brokers"`
	} `yaml:"kafka"`

	Clusters []cluster.ConfigCluster `yaml:"clusters"`

	Server struct {
		API struct {
			Enabled      bool   `yaml:"enabled"`
			Address      string `yaml:"address"`
			ReadTimeout  string `yaml:"read_timeout"`
			WriteTimeout string `yaml:"write_timeout"`
		} `yaml:"api"`
	} `yaml:"server"`
}

func main() {

	config, err := loadConfig("config.dev.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("Starting LagRadar...")

	checkInterval, err := time.ParseDuration(config.Collector.CheckInterval)
	if err != nil {
		log.Fatalf("Invalid check interval: %v", err)
	}

	inactivityTimeout, err := time.ParseDuration(config.Collector.Thresholds.InactivityTimeout)
	if err != nil {
		log.Fatalf("Invalid inactivity timeout: %v", err)
	}
	// Create global collector config
	globalCollectorConfig := collector.Config{
		WindowSize:             config.Collector.Window.Size,
		MinWindowSize:          config.Collector.Window.MinSize,
		CheckInterval:          checkInterval,
		StalledConsumptionRate: config.Collector.Thresholds.StalledConsumptionRate,
		RapidLagIncreaseRate:   config.Collector.Thresholds.RapidLagIncreaseRate,
		LagTrendThreshold:      config.Collector.Thresholds.LagTrendThreshold,
		InactivityTimeout:      inactivityTimeout,
		MaxConcurrency:         config.Collector.MaxConcurrency,
	}

	// Create cluster manager
	clusterManager := cluster.NewManager(globalCollectorConfig)
	defer clusterManager.Stop()

	if len(config.Clusters) > 0 {
		log.Printf("Multi-cluster mode: adding %d clusters", len(config.Clusters))
		for _, clusterConfig := range config.Clusters {
			if err := clusterManager.AddCluster(clusterConfig); err != nil {
				log.Printf("Failed to add cluster %s: %v", clusterConfig.Name, err)
			}
		}
	} else if len(config.Kafka.Brokers) > 0 {
		log.Printf("Single-cluster mode (legacy)")
		legacyCluster := cluster.ConfigCluster{
			Name:    "default",
			Enabled: true,
			Brokers: config.Kafka.Brokers,
		}
		if err := clusterManager.AddCluster(legacyCluster); err != nil {
			log.Fatalf("Failed to add legacy cluster: %v", err)
		}
	} else {
		log.Fatal("No Kafka clusters configured")
	}

	// Start HTTP server
	srv := startHTTPServer(config, clusterManager)

	// Wait for shutdown signal
	waitForShutdown()

	log.Println("Shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Shutdown complete")
}

func loadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &config, nil
}

func startHTTPServer(config *Config, manager *cluster.Manager) *http.Server {
	mux := http.NewServeMux()

	// Prometheus metrics endpoint
	mux.Handle(config.HTTP.MetricsPath, promhttp.Handler())

	// Health Ready endpoints - GLOBAL level for K8s
	mux.HandleFunc("/health", api.HealthHandler)
	mux.HandleFunc("/ready", api.ClusterReadyHandler(manager))

	// Create Multi-cluster handlers
	clusterHandlers := api.NewClusterHandlers(manager)

	// API endpoints
	mux.HandleFunc("/api/v1/status", clusterHandlers.GetAllGroupsAcrossClusters)
	mux.HandleFunc("/api/v1/groups", clusterHandlers.GetAllGroupsList)
	mux.HandleFunc("/api/v1/config", api.ConfigHandler(config))
	mux.HandleFunc("/api/v1/clusters", clusterHandlers.ListClusters)
	mux.HandleFunc("/api/v1/clusters/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/status"):
			clusterHandlers.GetClusterStatus(w, r)
		case strings.HasSuffix(path, "/groups"):
			clusterHandlers.GetClusterGroups(w, r)
		case strings.Contains(path, "/groups/"):
			clusterHandlers.GetClusterGroupStatus(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	// Legacy endpoints for backward compatibility - use the "default" cluster
	if defaultCluster, exists := manager.GetCluster("default"); exists {
		mux.HandleFunc("/api/v1/status/", api.GroupStatusHandler(defaultCluster.Collector))
	}

	// Log all registered routes
	log.Println("Registered routes:")
	log.Printf("  %s -> Prometheus metrics", config.HTTP.MetricsPath)
	log.Println("  /health -> Health check (K8s liveness)")
	log.Println("  /ready -> Readiness check (K8s readiness)")
	log.Println("  /api/v1/clusters -> List clusters")
	log.Println("  /api/v1/clusters/{cluster}/status -> Cluster status")
	log.Println("  /api/v1/clusters/{cluster}/groups -> Cluster groups")
	log.Println("  /api/v1/clusters/{cluster}/groups/{group} -> Group status")
	log.Println("  /api/v1/status -> All groups (aggregated)")
	log.Println("  /api/v1/groups -> List all groups")
	log.Println("  /api/v1/config -> Show config")

	// Determine server address
	serverAddr := config.HTTP.Address
	if config.Server.API.Address != "" {
		serverAddr = config.Server.API.Address
	}

	srv := &http.Server{
		Addr:         serverAddr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
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
