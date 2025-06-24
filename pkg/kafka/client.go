package kafka

import (
	"context"
	"fmt"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"time"
)

// Client wraps Kafka admin and consumer clients
type Client struct {
	admin    *kafka.AdminClient
	consumer *kafka.Consumer
	config   *Config
}

// Config holds Kafka client configuration
type Config struct {
	Brokers        string
	ConsumerGroup  string
	RequestTimeout time.Duration
}

// NewClient creates a new Kafka client
func NewClient(cfg *Config) (*Client, error) {
	if cfg.ConsumerGroup == "" {
		cfg.ConsumerGroup = "lagradar_metrics_collector"
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = 5 * time.Second
	}

	admin, err := kafka.NewAdminClient(&kafka.ConfigMap{
		"bootstrap.servers":  cfg.Brokers,
		"request.timeout.ms": int(cfg.RequestTimeout.Milliseconds()),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create admin client: %w", err)
	}

	consumer, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":  cfg.Brokers,
		"group.id":           cfg.ConsumerGroup,
		"auto.offset.reset":  "earliest",
		"enable.auto.commit": false,
	})
	if err != nil {
		admin.Close()
		return nil, fmt.Errorf("failed to create consumer: %w", err)
	}

	return &Client{
		admin:    admin,
		consumer: consumer,
		config:   cfg,
	}, nil
}

// Close closes all Kafka clients
func (c *Client) Close() {
	if c.admin != nil {
		c.admin.Close()
	}
	if c.consumer != nil {
		c.consumer.Close()
	}
}

// ListConsumerGroups returns all consumer groups
func (c *Client) ListConsumerGroups(ctx context.Context) ([]string, error) {
	result, err := c.admin.ListConsumerGroups(ctx)
	if err != nil {
		return nil, err
	}

	var groups []string
	for _, group := range result.Valid {
		groups = append(groups, group.GroupID)
	}
	return groups, nil
}

// GetConsumerGroupOffsets returns offsets for a consumer group
func (c *Client) GetConsumerGroupOffsets(ctx context.Context, groupID string) ([]TopicPartitionOffset, error) {
	offsetsResult, err := c.admin.ListConsumerGroupOffsets(ctx, []kafka.ConsumerGroupTopicPartitions{
		{
			Group: groupID,
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list offsets: %w", err)
	}

	if len(offsetsResult.ConsumerGroupsTopicPartitions) == 0 {
		return nil, nil
	}

	groupOffsets := offsetsResult.ConsumerGroupsTopicPartitions[0]

	var results []TopicPartitionOffset
	for _, tp := range groupOffsets.Partitions {
		if tp.Topic == nil {
			continue
		}

		results = append(results, TopicPartitionOffset{
			Topic:     *tp.Topic,
			Partition: tp.Partition,
			Offset:    tp.Offset,
		})
	}

	return results, nil
}

// GetHighWatermark returns the high watermark for a topic partition
func (c *Client) GetHighWatermark(topic string, partition int32) (int64, error) {
	_, high, err := c.consumer.QueryWatermarkOffsets(topic, partition, int(c.config.RequestTimeout.Milliseconds()))
	if err != nil {
		return 0, fmt.Errorf("failed to query watermark: %w", err)
	}
	return high, nil
}

// GetConsumerGroupMembers returns the number of members in a consumer group
func (c *Client) GetConsumerGroupMembers(ctx context.Context, groupIDs []string) (map[string]int, error) {
	result, err := c.admin.DescribeConsumerGroups(ctx, groupIDs)
	if err != nil {
		return nil, err
	}

	members := make(map[string]int)
	for _, desc := range result.ConsumerGroupDescriptions {
		if desc.Error.Code() == kafka.ErrNoError {
			members[desc.GroupID] = len(desc.Members)
		}
	}

	return members, nil
}

// TopicPartitionOffset represents topic partition offset information
type TopicPartitionOffset struct {
	Topic     string
	Partition int32
	Offset    kafka.Offset
}
