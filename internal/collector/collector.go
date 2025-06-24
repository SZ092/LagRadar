package collector

import (
	"LagRadar/internal/metrics"
	"context"
	"fmt"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"log"
	"strconv"
	"time"
)

type Collector struct {
	admin    *kafka.AdminClient
	consumer *kafka.Consumer
	brokers  string
}

func New(brokers string) (*Collector, error) {
	admin, err := kafka.NewAdminClient(&kafka.ConfigMap{
		"bootstrap.servers": brokers,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create admin client: %w", err)
	}

	consumer, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers": brokers,
		"group.id":          "lagradar_metrics_collector",
		"auto.offset.reset": "earliest",
	})
	if err != nil {
		admin.Close()
		return nil, fmt.Errorf("failed to create consumer: %w", err)
	}

	return &Collector{
		admin:    admin,
		consumer: consumer,
		brokers:  brokers,
	}, nil
}

func (c *Collector) Close() {
	if c.admin != nil {
		c.admin.Close()
	}
	if c.consumer != nil {
		c.consumer.Close()
	}
}

func (c *Collector) CollectMetrics(ctx context.Context) error {
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		metrics.ScrapeDuration.Set(duration)
	}()

	// List all consumer groups
	groupsResult, err := c.admin.ListConsumerGroups(ctx)
	if err != nil {
		metrics.ScrapeErrors.Inc()
		return fmt.Errorf("failed to list consumer groups: %w", err)
	}

	var groupIDs []string
	for _, group := range groupsResult.Valid {
		groupIDs = append(groupIDs, group.GroupID)
	}

	log.Printf("Found %d consumer groups", len(groupIDs))

	// Get group descriptions for member count
	groupDescResult, err := c.admin.DescribeConsumerGroups(ctx, groupIDs)
	if err != nil {
		log.Printf("Warning: failed to describe consumer groups: %v", err)
	} else {
		for _, groupDesc := range groupDescResult.ConsumerGroupDescriptions {
			if groupDesc.Error.Code() == kafka.ErrNoError {
				metrics.ConsumerGroupMembers.WithLabelValues(groupDesc.GroupID).Set(float64(len(groupDesc.Members)))
			}
		}
	}

	// Process each consumer group
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
	// Get consumer group offsets
	offsetsResult, err := c.admin.ListConsumerGroupOffsets(ctx, []kafka.ConsumerGroupTopicPartitions{
		{
			Group: groupID,
		},
	})

	if err != nil {
		return fmt.Errorf("failed to list offsets: %w", err)
	}

	if len(offsetsResult.ConsumerGroupsTopicPartitions) == 0 {
		return nil
	}

	groupOffsets := offsetsResult.ConsumerGroupsTopicPartitions[0]

	// Process each topic partition
	for _, topicPartition := range groupOffsets.Partitions {
		if topicPartition.Topic == nil {
			continue
		}

		topic := *topicPartition.Topic
		partition := topicPartition.Partition
		partitionStr := strconv.Itoa(int(partition))

		// Get high watermark
		_, highWatermark, err := c.consumer.QueryWatermarkOffsets(topic, partition, 5000)
		if err != nil {
			log.Printf("Failed to query watermark for %s[%d]: %v", topic, partition, err)
			continue
		}

		// Update log end offset metric
		metrics.LogEndOffset.WithLabelValues(topic, partitionStr).Set(float64(highWatermark))

		// Get committed offset
		committedOffset := topicPartition.Offset

		if committedOffset == kafka.OffsetInvalid {
			continue
		}

		// Update current offset metric
		metrics.ConsumerCurrentOffset.WithLabelValues(groupID, topic, partitionStr).Set(float64(committedOffset))

		// Calculate and update lag
		lag := int64(highWatermark) - int64(committedOffset)
		if lag < 0 {
			lag = 0
		}

		metrics.ConsumerLag.WithLabelValues(groupID, topic, partitionStr).Set(float64(lag))
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
