package consumer

import (
	"LagRadar/internal/rca"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-redis/redis/v8"
	"log"
	"time"
)

type Consumer struct {
	client   *redis.Client
	config   rca.ConsumerConfig
	handlers []EventHandler
}
type EventHandler interface {
	HandleEvent(event *rca.KafkaLagEvent) error
}

func NewConsumer(redisCfg rca.RedisConfig, consumerCfg rca.ConsumerConfig) (*Consumer, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     redisCfg.Addr,
		Password: redisCfg.Password,
		DB:       redisCfg.DB,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &Consumer{
		client:   client,
		config:   consumerCfg,
		handlers: make([]EventHandler, 0),
	}, nil
}

func (c *Consumer) RegisterHandler(handler EventHandler) {
	c.handlers = append(c.handlers, handler)
}

func (c *Consumer) Start(ctx context.Context) error {
	// Create consumer group if not exists
	err := c.client.XGroupCreateMkStream(ctx,
		c.config.StreamKey,
		c.config.ConsumerGroup,
		"0",
	).Err()

	if err != nil && err.Error() != "[Error] Consumer Group name already exists" {
		return fmt.Errorf("failed to create consumer group: %w", err)
	}

	log.Printf("Consumer started: group=%s, name=%s",
		c.config.ConsumerGroup,
		c.config.ConsumerName)

	// Main consume loop
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := c.consumeBatch(ctx); err != nil {
				log.Printf("Error consuming batch: %v", err)
				time.Sleep(time.Second)
			}
		}
	}
}

func (c *Consumer) consumeBatch(ctx context.Context) error {
	messages, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    c.config.ConsumerGroup,
		Consumer: c.config.ConsumerName,
		Streams:  []string{c.config.StreamKey, ">"},
		Count:    c.config.BatchSize,
		Block:    c.config.BlockTimeout,
	}).Result()

	if err != nil {
		if errors.Is(redis.Nil, err) {
			return nil
		}
		return fmt.Errorf("failed to read from stream: %w", err)
	}

	// Process messages
	for _, stream := range messages {
		for _, msg := range stream.Messages {
			if err := c.processMessage(ctx, msg); err != nil {
				log.Printf("Error processing message %s: %v", msg.ID, err)
				// TODO: Handle failed messages (DLQ, retry, etc.)
			} else {
				// ACK the message
				if err := c.client.XAck(ctx, c.config.StreamKey,
					c.config.ConsumerGroup, msg.ID).Err(); err != nil {
					log.Printf("Error ACKing message %s: %v", msg.ID, err)
				}
			}
		}
	}

	return nil
}

func (c *Consumer) processMessage(ctx context.Context, msg redis.XMessage) error {
	// Extract event data
	eventData, ok := msg.Values["event"].(string)
	if !ok {
		return fmt.Errorf("missing event data in message %s", msg.ID)
	}

	// Deserialize event
	var event rca.KafkaLagEvent
	if err := json.Unmarshal([]byte(eventData), &event); err != nil {
		return fmt.Errorf("failed to unmarshal event: %w", err)
	}

	// Process through all handlers
	for _, handler := range c.handlers {
		if err := handler.HandleEvent(&event); err != nil {
			log.Printf("Handler error: %v", err)
		}
	}

	return nil
}

func (c *Consumer) Close() error {
	return c.client.Close()
}
