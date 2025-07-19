package cluster

import (
	"LagRadar/internal/collector"
	"LagRadar/internal/rca"
	kafkaclient "LagRadar/pkg/kafka"
	"fmt"
	"log"
	"sync"
	"time"
)

// createCollectorWithRCA creates a collector with RCA support
func createCollectorWithRCA(brokers string, config collector.Config, rcaConfig *rca.PublisherConfig,
	clusterName string) (*collector.Collector, *rca.EventPublisher, error) {

	client, err := kafkaclient.NewClient(&kafkaclient.Config{
		Brokers:        brokers,
		ConsumerGroup:  "lagradar_metrics_collector",
		RequestTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	// Create standard collector
	coll := collector.NewWithClient(client, config)

	// If RCA is not enabled, return standard collector
	if rcaConfig == nil || !rcaConfig.Enabled {
		return coll, nil, nil
	}

	// Create RCA publisher
	rcaPublisher, err := rca.NewEventPublisher(*rcaConfig, clusterName)
	if err != nil {
		log.Printf("Failed to create RCA publisher: %v", err)
		return coll, nil, nil // Continue without RCA
	}

	log.Printf("Successfully created RCA publisher for cluster %s", clusterName)

	// Create RCA analyzer
	analyzer := &rcaAnalyzer{
		evaluator: rca.NewLagEvaluatorWithRCA(config, rcaPublisher),
		mu:        sync.Mutex{},
	}

	// Set evaluation hook
	coll.SetEvaluationHook(analyzer.analyzePartition)

	return coll, rcaPublisher, nil
}

// rcaAnalyzer wraps the RCA evaluator for use as a hook
type rcaAnalyzer struct {
	evaluator *rca.EvaluatorWithRCA
	mu        sync.Mutex
}

func (a *rcaAnalyzer) analyzePartition(status collector.PartitionConsumerStatus, records []collector.OffsetRecord) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// The evaluator will detect and publish events based on the status
	a.evaluator.EvaluatePartitionConsumerWithRCA(records, status.GroupID, status.Topic, status.Partition)
}
