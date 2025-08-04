package main

import (
	"LagRadar/internal/rca"
	"LagRadar/internal/rca/consumer"
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// SimpleLogHandler logs events for testing
type SimpleLogHandler struct{}

func (h *SimpleLogHandler) HandleEvent(event *rca.KafkaLagEvent) error {
	log.Printf("[EVENT] Type: %s, Severity: %s, Cluster: %s, Group: %s, Topic: %s, Partition: %d, Lag: %d",
		event.Type,
		event.Severity,
		event.Cluster,
		event.ConsumerGroup,
		event.Topic,
		event.Partition,
		event.CurrentLag,
	)
	return nil
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "rca_dev.yaml", "Path to config file")
	flag.Parse()

	cfg, err := rca.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Create consumer
	cons, err := consumer.NewConsumer(cfg.Redis, cfg.Consumer)
	if err != nil {
		log.Fatalf("Failed to create consumer: %v", err)
	}
	defer cons.Close()

	// Register simple log handler for testing
	logHandler := &SimpleLogHandler{}
	cons.RegisterHandler(logHandler)

	log.Printf("RCA Service started - Consumer Group: %s, Consumer: %s",
		cfg.Consumer.ConsumerGroup,
		cfg.Consumer.ConsumerName)

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down...")
		cancel()
	}()

	// Start consumer
	log.Printf("Starting RCA Consumer...")
	if err := cons.Start(ctx); err != nil {
		log.Fatalf("Consumer error: %v", err)
	}
}
