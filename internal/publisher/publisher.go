package publisher

import (
	"LagRadar/internal/collector"
	"LagRadar/internal/rca"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"log"
	"time"
)

type EventPublisher struct {
	client      *redis.Client
	enabled     bool
	source      string
	streamKey   string
	clusterName string
	config      rca.Config
}

func NewEventPublisher(config rca.Config, clusterName string) (*EventPublisher, error) {
	if !config.Publisher.Enabled {
		return &EventPublisher{
			enabled: false,
			config:  config,
		}, nil
	}

	client := redis.NewClient(&redis.Options{
		Addr:     config.Redis.Addr,
		Password: config.Redis.Password,
		DB:       config.Redis.DB,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &EventPublisher{
		client:      client,
		enabled:     config.Publisher.Enabled,
		source:      "lagradar",
		streamKey:   config.Publisher.StreamKey,
		clusterName: clusterName,
		config:      config,
	}, nil
}

func (p *EventPublisher) Close() {
	if p != nil && p.client != nil {
		p.client.Close()
	}
}

func (p *EventPublisher) PublishPartitionEvent(
	ctx context.Context,
	eventType string,
	severity rca.EventSeverity,
	status collector.PartitionConsumerStatus,
	message string,
) error {
	if p == nil || !p.enabled {
		return nil
	}

	event := rca.KafkaLagEvent{
		BaseEvent: rca.BaseEvent{
			ID:          generateEventID(),
			Source:      rca.SourceLagRadar,
			Type:        eventType,
			Severity:    severity,
			Timestamp:   time.Now(),
			Cluster:     p.clusterName,
			Title:       fmt.Sprintf("Kafka Consumer %s: %s", eventType, status.GroupID),
			Description: message,
			Data: map[string]interface{}{
				"window_completeness": status.WindowCompleteness,
				"health":              string(status.Health),
				"status":              string(status.Status),
				"lag_trend":           string(status.LagTrend),
				"is_active":           status.IsActive,
			},
		},
		ConsumerGroup:     status.GroupID,
		Topic:             status.Topic,
		Partition:         status.Partition,
		CurrentLag:        status.CurrentLag,
		CurrentOffset:     status.CurrentOffset,
		HighWatermark:     status.HighWatermark,
		ConsumptionRate:   status.ConsumptionRate,
		LagChangeRate:     status.LagChangeRate,
		TimeSinceLastMove: status.TimeSinceLastMove.String(),
	}

	// Simple deduplication check
	event.Fingerprint = p.generateFingerprint(event)
	data, err := json.Marshal(event)

	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	dedupeKey := fmt.Sprintf("rca:dedup:%s", event.Fingerprint)
	wasNew, err := p.client.SetNX(ctx, dedupeKey, "1", p.config.Publisher.DeDupeWindow).Result()
	if err != nil {
		log.Printf("Dedup check failed: %v", err)
	} else if !wasNew {
		log.Printf("[RCA] Event deduplicated: %s for group %s/%s[%d] (fingerprint: %s)",
			eventType, status.GroupID, status.Topic, status.Partition, event.Fingerprint)
		return nil
	}

	var lastErr error
	for i := 0; i < p.config.Publisher.MaxRetries; i++ {
		_, err = p.client.XAdd(ctx, &redis.XAddArgs{
			Stream: p.streamKey,
			// Set to 100K for now
			MaxLen: 100000,
			Approx: true,
			Values: map[string]interface{}{
				"event":     string(data),
				"type":      eventType,
				"severity":  string(severity),
				"cluster":   p.clusterName,
				"group_id":  status.GroupID,
				"topic":     status.Topic,
				"partition": fmt.Sprintf("%d", status.Partition),
				"timestamp": event.Timestamp.Unix(),
			},
		}).Result()

		if err == nil {
			log.Printf("[RCA] Published event: %s for group %s/%s[%d]",
				eventType, status.GroupID, status.Topic, status.Partition)
			return nil
		}

		lastErr = err
		if i < p.config.Publisher.MaxRetries-1 {
			retryInterval, _ := time.ParseDuration(p.config.Publisher.RetryInterval)
			time.Sleep(retryInterval * time.Duration(i+1))
		}
	}

	return fmt.Errorf("failed to publish event after %d retries: %w", p.config.Publisher.MaxRetries, lastErr)
}

func (p *EventPublisher) generateFingerprint(event rca.KafkaLagEvent) string {

	timeWindow := event.Timestamp.Truncate(p.config.Publisher.DeDupeWindow).Unix()
	data := fmt.Sprintf("%s:%s:%s:%s:%d:%d",
		event.Source,
		event.Type,
		event.ConsumerGroup,
		event.Topic,
		event.Partition,
		timeWindow,
	)
	return fmt.Sprintf("%x", md5.Sum([]byte(data)))
}

func generateEventID() string {
	return uuid.New().String()
}
