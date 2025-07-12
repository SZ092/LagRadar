package redis

import (
	"LagRadar/internal/collector"
	"context"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestEventPublisher(t *testing.T) {

	redisAddr := "localhost:6379"
	if !isRedisAvailable(redisAddr) {
		t.Skip("Redis is not available, skipping test")
	}

	config := PublisherConfig{
		Enabled:       true,
		RedisAddr:     redisAddr,
		StreamKey:     "test:events",
		MaxRetries:    3,
		RetryInterval: "100ms",
	}

	publisher, err := NewEventPublisher(config, "test-cluster")
	require.NoError(t, err, "Failed to create publisher")
	require.NotNil(t, publisher, "Publisher should not be nil")
	defer publisher.Close()

	// Test publishing event
	status := collector.PartitionConsumerStatus{
		GroupID:           "test-group",
		Topic:             "test-topic",
		Partition:         0,
		Status:            collector.StatusStopped,
		CurrentLag:        1000,
		CurrentOffset:     5000,
		HighWatermark:     6000,
		ConsumptionRate:   0,
		LagChangeRate:     100,
		TimeSinceLastMove: 5 * time.Minute,
		Health:            collector.HealthCritical,
		Message:           "Consumer stopped",
	}

	err = publisher.PublishPartitionEvent(
		context.Background(),
		EventTypeConsumerStopped,
		SeverityCritical,
		status,
		"Test message",
	)
	assert.NoError(t, err, "Failed to publish event")

	// Verify event was published
	client := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	defer client.Close()

	// Read from stream
	result, err := client.XRead(context.Background(), &redis.XReadArgs{
		Streams: []string{config.StreamKey, "0"},
		Count:   1,
		Block:   1 * time.Second,
	}).Result()

	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Len(t, result[0].Messages, 1)

	// Verify event content
	eventData := result[0].Messages[0].Values["event"]
	assert.NotNil(t, eventData)
}

func TestEventPublisherDisabled(t *testing.T) {
	config := PublisherConfig{
		Enabled: false,
	}

	publisher, err := NewEventPublisher(config, "test-cluster")
	assert.NoError(t, err)
	assert.NotNil(t, publisher)
	assert.False(t, publisher.enabled)

	// Should not error when publishing with disabled publisher
	err = publisher.PublishPartitionEvent(
		context.Background(),
		EventTypeConsumerStopped,
		SeverityCritical,
		collector.PartitionConsumerStatus{},
		"Test",
	)
	assert.NoError(t, err)
}

func TestEventPublisherConnectionFailure(t *testing.T) {
	config := PublisherConfig{
		Enabled:   true,
		RedisAddr: "localhost:9999", // Invalid port
	}

	publisher, err := NewEventPublisher(config, "test-cluster")
	assert.Error(t, err)
	assert.Nil(t, publisher)
}

func isRedisAvailable(addr string) bool {
	client := redis.NewClient(&redis.Options{
		Addr:        addr,
		DialTimeout: 2 * time.Second,
	})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := client.Ping(ctx).Result()
	return err == nil
}
