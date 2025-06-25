package collector

import (
	"LagRadar/internal/metrics"
	kafkaclient "LagRadar/pkg/kafka"
	"context"
	"fmt"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"log"
	"strconv"
	"time"
)

// KafkaClient defines the interface for Kafka operations
type KafkaClient interface {
	Close()
	ListConsumerGroups(ctx context.Context) ([]string, error)
	GetConsumerGroupOffsets(ctx context.Context, groupID string) ([]kafkaclient.TopicPartitionOffset, error)
	GetHighWatermark(topic string, partition int32) (int64, error)
	GetConsumerGroupMembers(ctx context.Context, groupIDs []string) (map[string]int, error)
}

type Collector struct {
	client KafkaClient
}

func New(brokers string) (*Collector, error) {
	client, err := kafkaclient.NewClient(&kafkaclient.Config{
		Brokers:        brokers,
		ConsumerGroup:  "lagradar_metrics_collector",
		RequestTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	return &Collector{
		client: client,
	}, nil
}

// NewWithClient creates a new collector with a custom client - For Test
func NewWithClient(client KafkaClient) *Collector {
	return &Collector{
		client: client,
	}
}

func (c *Collector) Close() {
	if c.client != nil {
		c.client.Close()
	}
}

func (c *Collector) CollectMetrics(ctx context.Context) error {
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		metrics.ScrapeDuration.Set(duration)
	}()

	// List all consumer groups
	groupIDs, err := c.client.ListConsumerGroups(ctx)
	if err != nil {
		metrics.ScrapeErrors.Inc()
		return fmt.Errorf("failed to list consumer groups: %w", err)
	}

	log.Printf("Found %d consumer groups", len(groupIDs))

	// Get group member counts
	memberCounts, err := c.client.GetConsumerGroupMembers(ctx, groupIDs)
	if err != nil {
		log.Printf("Warning: failed to get group members: %v", err)
	} else {
		for groupID, count := range memberCounts {
			metrics.ConsumerGroupMembers.WithLabelValues(groupID).Set(float64(count))
		}
	}

	for _, groupID := range groupIDs {
		if err := c.collectGroupMetrics(ctx, groupID); err != nil {
			log.Printf("Error collecting metrics for group %s: %v", groupID, err)
			metrics.ScrapeErrors.Inc()
			continue
		}
	}

	return nil
}

func (c *Collector) collectGroupMetrics(ctx context.Context, groupID string) error {
	offsets, err := c.client.GetConsumerGroupOffsets(ctx, groupID)
	if err != nil {
		return fmt.Errorf("failed to get group offsets: %w", err)
	}

	for _, tpo := range offsets {
		partitionStr := strconv.Itoa(int(tpo.Partition))

		highWatermark, err := c.client.GetHighWatermark(tpo.Topic, tpo.Partition)
		if err != nil {
			log.Printf("Failed to get high watermark for %s[%d]: %v", tpo.Topic, tpo.Partition, err)
			continue
		}

		metrics.LogEndOffset.WithLabelValues(tpo.Topic, partitionStr).Set(float64(highWatermark))

		// Skip if no committed offset
		if tpo.Offset == kafka.OffsetInvalid {
			continue
		}

		// Update current offset metric
		metrics.ConsumerCurrentOffset.WithLabelValues(groupID, tpo.Topic, partitionStr).Set(float64(tpo.Offset))

		// Calculate and update lag
		lag := highWatermark - int64(tpo.Offset)
		if lag < 0 {
			lag = 0
		}

		metrics.ConsumerLag.WithLabelValues(groupID, tpo.Topic, partitionStr).Set(float64(lag))
	}

	return nil
}

func (c *Collector) StartPeriodicCollection(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	if err := c.CollectMetrics(ctx); err != nil {
		log.Printf("Initial collection error: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("Stopping periodic collection")
			return
		case <-ticker.C:
			if err := c.CollectMetrics(ctx); err != nil {
				log.Printf("Collection error: %v", err)
			}
		}
	}
}

// PrintLagTable prints lag information in table format - CLI mode
func (c *Collector) PrintLagTable(ctx context.Context, groupFilter string) error {

	fmt.Printf("%-35s %-25s %-10s %-15s %-15s %-10s\n",
		"GROUP", "TOPIC", "PARTITION", "LOGEND", "COMMITTED", "LAG")
	fmt.Println("------------------------------------------------------------------------------------------------")

	groupIDs, err := c.client.ListConsumerGroups(ctx)
	if err != nil {
		return fmt.Errorf("failed to list consumer groups: %w", err)
	}

	if groupFilter != "" {
		filtered := []string{}
		for _, gid := range groupIDs {
			if gid == groupFilter {
				filtered = append(filtered, gid)
				break
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("consumer group '%s' not found", groupFilter)
		}
		groupIDs = filtered
	}

	// Process each group
	for _, groupID := range groupIDs {
		offsets, err := c.client.GetConsumerGroupOffsets(ctx, groupID)
		if err != nil {
			fmt.Printf("Error getting offsets for group %s: %v\n", groupID, err)
			continue
		}

		for _, tpo := range offsets {
			highWatermark, err := c.client.GetHighWatermark(tpo.Topic, tpo.Partition)
			if err != nil {
				fmt.Printf("Error getting high watermark for %s[%d]: %v\n",
					tpo.Topic, tpo.Partition, err)
				continue
			}

			var lag int64
			var committedStr string

			if tpo.Offset == kafka.OffsetInvalid {
				committedStr = "NO_COMMIT"
				lag = highWatermark
			} else {
				committedStr = fmt.Sprintf("%d", tpo.Offset)
				lag = highWatermark - int64(tpo.Offset)
				if lag < 0 {
					lag = 0
				}
			}

			fmt.Printf("%-35s %-25s %-10d %-15d %-15s %-10d\n",
				groupID, tpo.Topic, tpo.Partition, highWatermark, committedStr, lag)
		}
	}

	return nil
}
