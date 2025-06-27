package collector

import (
	"LagRadar/internal/metrics"
	kafkaclient "LagRadar/pkg/kafka"
	"context"
	"errors"
	"fmt"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"testing"
	"time"
)

// MockKafkaClient is a mock implementation of the Kafka client
type MockKafkaClient struct {
	mock.Mock
}

func (m *MockKafkaClient) Close() {
	m.Called()
}

func (m *MockKafkaClient) ListConsumerGroups(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockKafkaClient) GetConsumerGroupOffsets(ctx context.Context, groupID string) ([]kafkaclient.TopicPartitionOffset, error) {
	args := m.Called(ctx, groupID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]kafkaclient.TopicPartitionOffset), args.Error(1)
}

func (m *MockKafkaClient) GetHighWatermark(topic string, partition int32) (int64, error) {
	args := m.Called(topic, partition)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockKafkaClient) GetConsumerGroupMembers(ctx context.Context, groupIDs []string) (map[string]int, error) {
	args := m.Called(ctx, groupIDs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]int), args.Error(1)
}

// Test helper to create a collector with a mock client
func newTestCollector(mockClient *MockKafkaClient) *Collector {
	return NewWithClient(mockClient)
}

// TestCollectMetrics_Success tests successful metrics collection
func TestCollectMetrics_Success(t *testing.T) {
	// Reset gauge metrics for clean test
	metrics.ConsumerLag.Reset()
	metrics.ConsumerCurrentOffset.Reset()
	metrics.LogEndOffset.Reset()
	metrics.ConsumerGroupMembers.Reset()

	mockClient := new(MockKafkaClient)
	collector := newTestCollector(mockClient)

	ctx := context.Background()

	// Setup mock expectations
	consumerGroups := []string{"test-group-1", "test-group-2"}
	mockClient.On("ListConsumerGroups", ctx).Return(consumerGroups, nil)

	memberCounts := map[string]int{
		"test-group-1": 3,
		"test-group-2": 2,
	}
	mockClient.On("GetConsumerGroupMembers", ctx, consumerGroups).Return(memberCounts, nil)

	group1Offsets := []kafkaclient.TopicPartitionOffset{
		{Topic: "test-topic-1", Partition: 0, Offset: 100},
		{Topic: "test-topic-1", Partition: 1, Offset: 200},
		{Topic: "test-topic-2", Partition: 0, Offset: 50},
	}
	mockClient.On("GetConsumerGroupOffsets", ctx, "test-group-1").Return(group1Offsets, nil)

	group2Offsets := []kafkaclient.TopicPartitionOffset{
		{Topic: "test-topic-1", Partition: 0, Offset: 90},
		{Topic: "test-topic-3", Partition: 0, Offset: kafka.OffsetInvalid}, // No commits yet
	}
	mockClient.On("GetConsumerGroupOffsets", ctx, "test-group-2").Return(group2Offsets, nil)

	// Mock high watermarks
	mockClient.On("GetHighWatermark", "test-topic-1", int32(0)).Return(int64(150), nil)
	mockClient.On("GetHighWatermark", "test-topic-1", int32(1)).Return(int64(250), nil)
	mockClient.On("GetHighWatermark", "test-topic-2", int32(0)).Return(int64(100), nil)
	mockClient.On("GetHighWatermark", "test-topic-3", int32(0)).Return(int64(300), nil)

	err := collector.CollectMetrics(ctx)
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)

	// Check lags
	assert.Equal(t, float64(50), testutil.ToFloat64(metrics.ConsumerLag.WithLabelValues("test-group-1", "test-topic-1", "0")))
	assert.Equal(t, float64(50), testutil.ToFloat64(metrics.ConsumerLag.WithLabelValues("test-group-1", "test-topic-1", "1")))
	assert.Equal(t, float64(50), testutil.ToFloat64(metrics.ConsumerLag.WithLabelValues("test-group-1", "test-topic-2", "0")))
	assert.Equal(t, float64(60), testutil.ToFloat64(metrics.ConsumerLag.WithLabelValues("test-group-2", "test-topic-1", "0")))

	// Check member counts
	assert.Equal(t, float64(3), testutil.ToFloat64(metrics.ConsumerGroupMembers.WithLabelValues("test-group-1")))
	assert.Equal(t, float64(2), testutil.ToFloat64(metrics.ConsumerGroupMembers.WithLabelValues("test-group-2")))

	// Check scrape duration was set
	assert.Greater(t, testutil.ToFloat64(metrics.ScrapeDuration), float64(0))
}

// TestCollectMetrics_ListGroupsError tests error handling when listing groups fails
func TestCollectMetrics_ListGroupsError(t *testing.T) {
	initialErrors := testutil.ToFloat64(metrics.ScrapeErrors)

	mockClient := new(MockKafkaClient)
	collector := newTestCollector(mockClient)

	ctx := context.Background()

	mockClient.On("ListConsumerGroups", ctx).Return([]string(nil), errors.New("kafka connection failed"))

	err := collector.CollectMetrics(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list consumer groups")
	mockClient.AssertExpectations(t)

	// Verify error counter was incremented
	currentErrors := testutil.ToFloat64(metrics.ScrapeErrors)
	assert.Equal(t, initialErrors+1, currentErrors)
}

// TestCollectMetrics_PartialGroupError tests handling of errors for individual groups
func TestCollectMetrics_PartialGroupError(t *testing.T) {

	initialErrors := testutil.ToFloat64(metrics.ScrapeErrors)
	metrics.ConsumerLag.Reset()

	mockClient := new(MockKafkaClient)
	collector := newTestCollector(mockClient)

	ctx := context.Background()

	consumerGroups := []string{"good-group", "bad-group"}
	mockClient.On("ListConsumerGroups", ctx).Return(consumerGroups, nil)

	// Mock member counts - return error but continue
	mockClient.On("GetConsumerGroupMembers", ctx, consumerGroups).Return(map[string]int(nil), errors.New("partial error"))

	// Good group succeeds
	goodOffsets := []kafkaclient.TopicPartitionOffset{
		{Topic: "test-topic", Partition: 0, Offset: 100},
	}
	mockClient.On("GetConsumerGroupOffsets", ctx, "good-group").Return(goodOffsets, nil)
	mockClient.On("GetHighWatermark", "test-topic", int32(0)).Return(int64(150), nil)

	// Bad group fails
	mockClient.On("GetConsumerGroupOffsets", ctx, "bad-group").Return([]kafkaclient.TopicPartitionOffset(nil), errors.New("group not found"))

	err := collector.CollectMetrics(ctx)
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)

	// Verify good group metrics were collected
	assert.Equal(t, float64(50), testutil.ToFloat64(metrics.ConsumerLag.WithLabelValues("good-group", "test-topic", "0")))

	// Verify error counter was incremented for bad group
	currentErrors := testutil.ToFloat64(metrics.ScrapeErrors)
	assert.Equal(t, initialErrors+1, currentErrors)
}

// TestCollectMetrics_HighWatermarkError tests handling of high watermark fetch errors
func TestCollectMetrics_HighWatermarkError(t *testing.T) {
	metrics.ConsumerLag.Reset()
	metrics.LogEndOffset.Reset()

	mockClient := new(MockKafkaClient)
	collector := newTestCollector(mockClient)

	ctx := context.Background()

	consumerGroups := []string{"test-group"}
	mockClient.On("ListConsumerGroups", ctx).Return(consumerGroups, nil)
	mockClient.On("GetConsumerGroupMembers", ctx, consumerGroups).Return(map[string]int{"test-group": 1}, nil)

	// Mock offsets
	offsets := []kafkaclient.TopicPartitionOffset{
		{Topic: "test-topic", Partition: 0, Offset: 100},
		{Topic: "test-topic", Partition: 1, Offset: 200},
	}
	mockClient.On("GetConsumerGroupOffsets", ctx, "test-group").Return(offsets, nil)

	// First partition succeeds, second fails
	mockClient.On("GetHighWatermark", "test-topic", int32(0)).Return(int64(150), nil)
	mockClient.On("GetHighWatermark", "test-topic", int32(1)).Return(int64(0), errors.New("partition not found"))

	err := collector.CollectMetrics(ctx)
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)

	// Verify only successful partition has metrics
	assert.Equal(t, float64(50), testutil.ToFloat64(metrics.ConsumerLag.WithLabelValues("test-group", "test-topic", "0")))

	// Verify that log end offset was only recorded for successful partition
	assert.Equal(t, float64(150), testutil.ToFloat64(metrics.LogEndOffset.WithLabelValues("test-topic", "0")))
}

// TestCollectMetrics_InvalidOffset tests handling of invalid offsets
func TestCollectMetrics_InvalidOffset(t *testing.T) {
	metrics.LogEndOffset.Reset()

	mockClient := new(MockKafkaClient)
	collector := newTestCollector(mockClient)

	ctx := context.Background()

	// Setup mock expectations with mixed valid and invalid offsets
	consumerGroups := []string{"test-group"}
	mockClient.On("ListConsumerGroups", ctx).Return(consumerGroups, nil)
	mockClient.On("GetConsumerGroupMembers", ctx, consumerGroups).Return(map[string]int{"test-group": 1}, nil)

	// Mock offsets with both valid and invalid offsets
	offsets := []kafkaclient.TopicPartitionOffset{
		{Topic: "test-topic", Partition: 0, Offset: kafka.OffsetInvalid},
		{Topic: "test-topic", Partition: 1, Offset: 150},
	}
	mockClient.On("GetConsumerGroupOffsets", ctx, "test-group").Return(offsets, nil)
	mockClient.On("GetHighWatermark", "test-topic", int32(0)).Return(int64(100), nil)
	mockClient.On("GetHighWatermark", "test-topic", int32(1)).Return(int64(200), nil)

	err := collector.CollectMetrics(ctx)
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)

	// Verify that valid offset has lag recorded
	assert.Equal(t, float64(50), testutil.ToFloat64(metrics.ConsumerLag.WithLabelValues("test-group", "test-topic", "1")))
	assert.Equal(t, float64(150), testutil.ToFloat64(metrics.ConsumerCurrentOffset.WithLabelValues("test-group", "test-topic", "1")))

	// Log end offset should be recorded for both partitions
	assert.Equal(t, float64(100), testutil.ToFloat64(metrics.LogEndOffset.WithLabelValues("test-topic", "0")))
	assert.Equal(t, float64(200), testutil.ToFloat64(metrics.LogEndOffset.WithLabelValues("test-topic", "1")))

}

// TestStartPeriodicCollection tests the periodic collection loop
func TestStartPeriodicCollection(t *testing.T) {
	mockClient := new(MockKafkaClient)
	collector := newTestCollector(mockClient)

	ctx, cancel := context.WithCancel(context.Background())

	consumerGroups := []string{"test-group"}
	mockClient.On("ListConsumerGroups", mock.Anything).Return(consumerGroups, nil).Times(3)
	mockClient.On("GetConsumerGroupMembers", mock.Anything, consumerGroups).Return(map[string]int{"test-group": 1}, nil).Times(3)
	mockClient.On("GetConsumerGroupOffsets", mock.Anything, "test-group").Return([]kafkaclient.TopicPartitionOffset{}, nil).Times(3)

	// Start periodic collection with 100 ms interval
	done := make(chan bool)
	go func() {
		collector.StartPeriodicCollection(ctx, 100*time.Millisecond)
		done <- true
	}()

	time.Sleep(250 * time.Millisecond)
	cancel()

	// Wait for goroutine to finish
	select {
	case <-done:

	case <-time.After(1 * time.Second):
		t.Fatal("StartPeriodicCollection did not stop in time")
	}

	// Verify it was called at least 2 times - Initial + at least 2 periodic = 3
	mockClient.AssertNumberOfCalls(t, "ListConsumerGroups", 3)
}

// Benchmark test
func BenchmarkCollectMetrics(b *testing.B) {
	mockClient := new(MockKafkaClient)
	collector := newTestCollector(mockClient)

	ctx := context.Background()

	// Setup mock for benchmark
	consumerGroups := make([]string, 100)
	for i := 0; i < 100; i++ {
		consumerGroups[i] = fmt.Sprintf("test-group-%d", i)
	}

	mockClient.On("ListConsumerGroups", ctx).Return(consumerGroups, nil)
	mockClient.On("GetConsumerGroupMembers", ctx, mock.Anything).Return(map[string]int{}, nil)

	// Each group has 10 topic-partitions
	for _, group := range consumerGroups {
		offsets := make([]kafkaclient.TopicPartitionOffset, 10)
		for j := 0; j < 10; j++ {
			offsets[j] = kafkaclient.TopicPartitionOffset{
				Topic:     fmt.Sprintf("topic-%d", j),
				Partition: 0,
				Offset:    100,
			}
		}
		mockClient.On("GetConsumerGroupOffsets", ctx, group).Return(offsets, nil)

		for j := 0; j < 10; j++ {
			mockClient.On("GetHighWatermark", fmt.Sprintf("topic-%d", j), int32(0)).Return(int64(200), nil)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = collector.CollectMetrics(ctx)
	}
}
