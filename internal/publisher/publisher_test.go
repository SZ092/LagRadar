package publisher

import (
	"LagRadar/internal/collector"
	"LagRadar/internal/rca"
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

	config := rca.Config{
		Publisher: rca.PublisherConfig{
			Enabled:       true,
			StreamKey:     "test:events",
			MaxRetries:    3,
			RetryInterval: "100ms",
			DeDupeWindow:  5 * time.Minute,
		},
		Redis: rca.RedisConfig{
			Addr:     redisAddr,
			Password: "",
			DB:       0,
		},
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
		rca.EventTypeConsumerStopped,
		rca.SeverityCritical,
		status,
		"Test message",
	)
	assert.NoError(t, err, "Failed to publish event")

	// Verify event was published
	client := redis.NewClient(&redis.Options{
		Addr:     config.Redis.Addr,
		Password: config.Redis.Password,
		DB:       config.Redis.DB,
	})
	defer client.Close()

	// Read from stream
	result, err := client.XRead(context.Background(), &redis.XReadArgs{
		Streams: []string{config.Publisher.StreamKey, "0"},
		Count:   1,
		Block:   1 * time.Second,
	}).Result()

	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Len(t, result[0].Messages, 1)

	// Verify event content
	eventData := result[0].Messages[0].Values["event"]
	assert.NotNil(t, eventData)

	// Verify event metadata
	assert.Equal(t, "consumer_stopped", result[0].Messages[0].Values["type"])
	assert.Equal(t, "critical", result[0].Messages[0].Values["severity"])
	assert.Equal(t, "test-cluster", result[0].Messages[0].Values["cluster"])
	assert.Equal(t, "test-group", result[0].Messages[0].Values["group_id"])
	assert.Equal(t, "test-topic", result[0].Messages[0].Values["topic"])
	assert.Equal(t, "0", result[0].Messages[0].Values["partition"])

}

func TestEventPublisherDisabled(t *testing.T) {
	config := rca.Config{
		Publisher: rca.PublisherConfig{
			Enabled: false,
		},
		Redis: rca.RedisConfig{
			Addr: "localhost:6379",
		},
	}

	publisher, err := NewEventPublisher(config, "test-cluster")
	assert.NoError(t, err)
	assert.NotNil(t, publisher)
	assert.False(t, publisher.enabled)

	// Should not error when publishing with disabled publisher
	err = publisher.PublishPartitionEvent(
		context.Background(),
		rca.EventTypeConsumerStopped,
		rca.SeverityCritical,
		collector.PartitionConsumerStatus{},
		"Test",
	)
	assert.NoError(t, err)
}

func TestEventPublisherConnectionFailure(t *testing.T) {
	config := rca.Config{
		Publisher: rca.PublisherConfig{
			Enabled:   true,
			StreamKey: "test:events",
		},
		Redis: rca.RedisConfig{
			Addr:     "localhost:9999", // Invalid port
			Password: "",
			DB:       0,
		},
	}

	publisher, err := NewEventPublisher(config, "test-cluster")
	assert.Error(t, err)
	assert.Nil(t, publisher)
}

func TestEventDeduplication(t *testing.T) {
	redisAddr := "localhost:6379"
	if !isRedisAvailable(redisAddr) {
		t.Skip("Redis is not available, skipping test")
	}

	config := rca.Config{
		Publisher: rca.PublisherConfig{
			Enabled:       true,
			StreamKey:     "test:dedup:events",
			MaxRetries:    3,
			RetryInterval: "100ms",
			DeDupeWindow:  1 * time.Minute,
		},
		Redis: rca.RedisConfig{
			Addr: redisAddr,
		},
	}
	// Clean up before test
	cleanupRedis(t, config)

	publisher, err := NewEventPublisher(config, "test-cluster")
	require.NoError(t, err)
	defer publisher.Close()

	// Create identical events
	status := collector.PartitionConsumerStatus{
		GroupID:   "test-group",
		Topic:     "test-topic",
		Partition: 0,
		Status:    collector.StatusStopped,
	}

	// First event should succeed
	err = publisher.PublishPartitionEvent(
		context.Background(),
		rca.EventTypeConsumerStopped,
		rca.SeverityCritical,
		status,
		"Duplicate test",
	)
	assert.NoError(t, err)

	// Second identical event should be deduplicated
	err = publisher.PublishPartitionEvent(
		context.Background(),
		rca.EventTypeConsumerStopped,
		rca.SeverityCritical,
		status,
		"Duplicate test",
	)
	assert.NoError(t, err) // Should not error, just skip

	// Verify only one event in stream
	client := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	defer client.Close()

	result, err := client.XRead(context.Background(), &redis.XReadArgs{
		Streams: []string{config.Publisher.StreamKey, "0"},
		Count:   10,
		Block:   1 * time.Second,
	}).Result()

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Len(t, result[0].Messages, 1, "Should have only one event due to deduplication")
}

// Benchmark test
func BenchmarkEventPublishing(b *testing.B) {
	redisAddr := "localhost:6379"
	if !isRedisAvailable(redisAddr) {
		b.Skip("Redis is not available, skipping benchmark")
	}

	config := rca.Config{
		Publisher: rca.PublisherConfig{
			Enabled:       true,
			StreamKey:     "bench:events",
			MaxRetries:    1,
			RetryInterval: "100ms",
			DeDupeWindow:  5 * time.Minute,
		},
		Redis: rca.RedisConfig{
			Addr: redisAddr,
		},
	}

	// Clean up before benchmark
	cleanupRedis(b, config)

	publisher, err := NewEventPublisher(config, "bench-cluster")
	require.NoError(b, err)
	defer publisher.Close()
	defer cleanupRedis(b, config)

	status := collector.PartitionConsumerStatus{
		GroupID:   "bench-group",
		Topic:     "bench-topic",
		Partition: 0,
		Status:    collector.StatusActive,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Vary the partition to avoid deduplication
		status.Partition = int32(i % 100)
		_ = publisher.PublishPartitionEvent(
			context.Background(),
			rca.EventTypeConsumerStopped,
			rca.SeverityWarning,
			status,
			"Benchmark event",
		)
	}
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

// Helper function to clean up Redis data before tests
func cleanupRedis(t testing.TB, config rca.Config) {
	client := redis.NewClient(&redis.Options{
		Addr:     config.Redis.Addr,
		Password: config.Redis.Password,
		DB:       config.Redis.DB,
	})
	defer client.Close()

	ctx := context.Background()

	// Delete the stream
	_ = client.Del(ctx, config.Publisher.StreamKey).Err()

	// Delete all dedup keys (pattern: rca:dedup:*)
	keys, err := client.Keys(ctx, "rca:dedup:*").Result()
	if err == nil && len(keys) > 0 {
		_ = client.Del(ctx, keys...).Err()
	}

	t.Logf("Cleaned up Redis: deleted stream %s and %d dedup keys",
		config.Publisher.StreamKey, len(keys))
}
