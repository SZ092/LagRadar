package consumer

import (
	"LagRadar/internal/rca"
	"context"
	"encoding/json"
	"errors"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

// MockEventHandler for testing
type MockEventHandler struct {
	mock.Mock
	mu             sync.Mutex
	receivedEvents []*rca.KafkaLagEvent
}

func (m *MockEventHandler) HandleEvent(event *rca.KafkaLagEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	args := m.Called(event)
	m.receivedEvents = append(m.receivedEvents, event)
	return args.Error(0)
}

func (m *MockEventHandler) GetReceivedEvents() []*rca.KafkaLagEvent {
	m.mu.Lock()
	defer m.mu.Unlock()

	events := make([]*rca.KafkaLagEvent, len(m.receivedEvents))
	copy(events, m.receivedEvents)
	return events
}

// Test NewConsumer
func TestNewConsumer(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	tests := []struct {
		name        string
		redisCfg    rca.RedisConfig
		consumerCfg rca.ConsumerConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "successful connection",
			redisCfg: rca.RedisConfig{
				Addr:     mr.Addr(),
				Password: "",
				DB:       0,
			},
			consumerCfg: rca.ConsumerConfig{
				ConsumerGroup: "test-group",
				StreamKey:     "test-stream",
				ConsumerName:  "test-consumer",
				BatchSize:     10,
				BlockTimeout:  1 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "connection failure",
			redisCfg: rca.RedisConfig{
				Addr:     "invalid:9999",
				Password: "",
				DB:       0,
			},
			consumerCfg: rca.ConsumerConfig{
				ConsumerGroup: "test-group",
			},
			wantErr:     true,
			errContains: "failed to connect to Redis",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			consumer, err := NewConsumer(tt.redisCfg, tt.consumerCfg)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Nil(t, consumer)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, consumer)

				// Cleanup
				if consumer != nil {
					err := consumer.Close()
					require.NoError(t, err)
				}
			}
		})
	}
}

// Test RegisterHandler
func TestRegisterHandler(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	consumer, err := NewConsumer(
		rca.RedisConfig{Addr: mr.Addr()},
		rca.ConsumerConfig{},
	)
	require.NoError(t, err)
	defer consumer.Close()

	// Register multiple handlers
	handler1 := &MockEventHandler{}
	handler2 := &MockEventHandler{}
	handler3 := &MockEventHandler{}

	consumer.RegisterHandler(handler1)
	consumer.RegisterHandler(handler2)
	consumer.RegisterHandler(handler3)

	assert.Len(t, consumer.handlers, 3)
}

// Test Start with context cancellation
func TestStartWithContextCancellation(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	consumer, err := NewConsumer(
		rca.RedisConfig{Addr: mr.Addr()},
		rca.ConsumerConfig{
			StreamKey:     "test-stream",
			ConsumerGroup: "test-group",
			ConsumerName:  "test-consumer",
			BatchSize:     10,
			BlockTimeout:  100 * time.Millisecond,
		},
	)
	require.NoError(t, err)
	defer consumer.Close()

	// Start consumer with cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	errChan := make(chan error, 1)
	go func() {
		errChan <- consumer.Start(ctx)
	}()

	time.Sleep(200 * time.Millisecond)

	// Cancel context
	cancel()

	// Should return context error
	select {
	case err := <-errChan:
		assert.Equal(t, context.Canceled, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Consumer did not stop after context cancellation")
	}
}

// Test consumeBatch with messages
func TestConsumeBatch(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	// Create Redis client for test setup
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	ctx := context.Background()
	streamKey := "test-stream"
	consumerGroup := "test-group"

	// Create consumer
	consumer, err := NewConsumer(
		rca.RedisConfig{Addr: mr.Addr()},
		rca.ConsumerConfig{
			StreamKey:     streamKey,
			ConsumerGroup: consumerGroup,
			ConsumerName:  "test-consumer",
			BatchSize:     10,
			BlockTimeout:  100 * time.Millisecond,
		},
	)
	require.NoError(t, err)
	defer consumer.Close()

	// Setup handler
	handler := &MockEventHandler{}
	consumer.RegisterHandler(handler)

	// Create stream and consumer group
	err = client.XGroupCreateMkStream(ctx, streamKey, consumerGroup, "0").Err()
	require.NoError(t, err)

	// Add test events to stream
	testEvents := []rca.KafkaLagEvent{
		{
			BaseEvent: rca.BaseEvent{
				ID:       "event-1",
				Type:     "consumer_stopped",
				Severity: rca.SeverityCritical,
			},
			ConsumerGroup: "test-group-1",
			Topic:         "test-topic",
			Partition:     0,
			CurrentLag:    1000,
		},
		{
			BaseEvent: rca.BaseEvent{
				ID:       "event-2",
				Type:     "rapid_lag_increase",
				Severity: rca.SeverityWarning,
			},
			ConsumerGroup: "test-group-2",
			Topic:         "test-topic",
			Partition:     1,
			CurrentLag:    5000,
		},
	}

	// Add events to stream
	for _, event := range testEvents {
		eventJSON, err := json.Marshal(event)
		require.NoError(t, err)

		_, err = client.XAdd(ctx, &redis.XAddArgs{
			Stream: streamKey,
			Values: map[string]interface{}{
				"event": string(eventJSON),
			},
		}).Result()
		require.NoError(t, err)
	}

	// Setup handler expectations
	handler.On("HandleEvent", mock.AnythingOfType("*rca.KafkaLagEvent")).Return(nil).Times(2)

	// Consume batch
	err = consumer.consumeBatch(ctx)
	assert.NoError(t, err)

	// Verify handler was called
	handler.AssertExpectations(t)

	// Verify received events
	receivedEvents := handler.GetReceivedEvents()
	assert.Len(t, receivedEvents, 2)
	assert.Equal(t, "event-1", receivedEvents[0].ID)
	assert.Equal(t, "event-2", receivedEvents[1].ID)
}

// Test processMessage with valid event
func TestProcessMessageValid(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	consumer, err := NewConsumer(
		rca.RedisConfig{Addr: mr.Addr()},
		rca.ConsumerConfig{},
	)
	require.NoError(t, err)
	defer consumer.Close()

	// Setup handlers
	handler1 := &MockEventHandler{}
	handler2 := &MockEventHandler{}
	consumer.RegisterHandler(handler1)
	consumer.RegisterHandler(handler2)

	// Create test event
	testEvent := rca.KafkaLagEvent{
		BaseEvent: rca.BaseEvent{
			ID:   "test-event",
			Type: "consumer_stopped",
		},
		ConsumerGroup: "test-group",
		CurrentLag:    1000,
	}

	eventJSON, err := json.Marshal(testEvent)
	require.NoError(t, err)

	// Create message
	msg := redis.XMessage{
		ID: "123-0",
		Values: map[string]interface{}{
			"event": string(eventJSON),
		},
	}

	// Setup expectations
	handler1.On("HandleEvent", mock.AnythingOfType("*rca.KafkaLagEvent")).Return(nil)
	handler2.On("HandleEvent", mock.AnythingOfType("*rca.KafkaLagEvent")).Return(nil)

	// Process message
	err = consumer.processMessage(context.Background(), msg)

	assert.NoError(t, err)
	handler1.AssertExpectations(t)
	handler2.AssertExpectations(t)
}

// Test processMessage with invalid event data
func TestProcessMessageInvalidData(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	consumer, err := NewConsumer(
		rca.RedisConfig{Addr: mr.Addr()},
		rca.ConsumerConfig{},
	)
	require.NoError(t, err)
	defer consumer.Close()

	tests := []struct {
		name        string
		message     redis.XMessage
		errContains string
	}{
		{
			name: "missing event field",
			message: redis.XMessage{
				ID:     "123-0",
				Values: map[string]interface{}{},
			},
			errContains: "missing event data",
		},
		{
			name: "invalid JSON",
			message: redis.XMessage{
				ID: "123-1",
				Values: map[string]interface{}{
					"event": "invalid json{",
				},
			},
			errContains: "failed to unmarshal event",
		},
		{
			name: "wrong type for event field",
			message: redis.XMessage{
				ID: "123-2",
				Values: map[string]interface{}{
					"event": 12345, // Not a string
				},
			},
			errContains: "missing event data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := consumer.processMessage(context.Background(), tt.message)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

// Test handler error handling
func TestHandlerErrorHandling(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	consumer, err := NewConsumer(
		rca.RedisConfig{Addr: mr.Addr()},
		rca.ConsumerConfig{},
	)
	require.NoError(t, err)
	defer consumer.Close()

	// Setup handlers - one fails, one succeeds
	failingHandler := &MockEventHandler{}
	successHandler := &MockEventHandler{}
	consumer.RegisterHandler(failingHandler)
	consumer.RegisterHandler(successHandler)

	// Create test event
	testEvent := rca.KafkaLagEvent{
		BaseEvent: rca.BaseEvent{ID: "test"},
	}
	eventJSON, _ := json.Marshal(testEvent)

	msg := redis.XMessage{
		ID: "123-0",
		Values: map[string]interface{}{
			"event": string(eventJSON),
		},
	}

	// Setup expectations
	failingHandler.On("HandleEvent", mock.Anything).Return(errors.New("handler failed"))
	successHandler.On("HandleEvent", mock.Anything).Return(nil)

	// Process should not return error even if handler fails
	err = consumer.processMessage(context.Background(), msg)
	assert.NoError(t, err)

	// Both handlers should be called
	failingHandler.AssertExpectations(t)
	successHandler.AssertExpectations(t)
}

// Test Close
func TestClose(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	consumer, err := NewConsumer(
		rca.RedisConfig{Addr: mr.Addr()},
		rca.ConsumerConfig{},
	)
	require.NoError(t, err)

	// Close should not error
	err = consumer.Close()
	assert.NoError(t, err)

	// Operations after close should fail
	ctx := context.Background()
	err = consumer.consumeBatch(ctx)
	assert.Error(t, err)
}

// Benchmark consumer performance
func BenchmarkProcessMessage(b *testing.B) {
	mr, err := miniredis.Run()
	require.NoError(b, err)
	defer mr.Close()

	consumer, err := NewConsumer(
		rca.RedisConfig{Addr: mr.Addr()},
		rca.ConsumerConfig{},
	)
	require.NoError(b, err)
	defer consumer.Close()

	// No-op handler
	handler := &MockEventHandler{}
	handler.On("HandleEvent", mock.Anything).Return(nil)
	consumer.RegisterHandler(handler)

	// Create test message
	event := rca.KafkaLagEvent{
		BaseEvent: rca.BaseEvent{
			ID:        "bench-event",
			Type:      "benchmark",
			Timestamp: time.Now(),
		},
		ConsumerGroup: "bench-group",
		Topic:         "bench-topic",
		Partition:     0,
		CurrentLag:    1000,
	}
	eventJSON, _ := json.Marshal(event)

	msg := redis.XMessage{
		ID: "123-0",
		Values: map[string]interface{}{
			"event": string(eventJSON),
		},
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = consumer.processMessage(ctx, msg)
	}
}
