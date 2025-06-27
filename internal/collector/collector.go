package collector

import (
	"LagRadar/internal/metrics"
	kafkaclient "LagRadar/pkg/kafka"
	"context"
	"fmt"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"log"
	"strconv"
	"sync"
	"time"
)

// ConsumerStatus represents the health status of a consumer
type ConsumerStatus string

const (
	StatusOK      ConsumerStatus = "OK"
	StatusWarning ConsumerStatus = "WARNING"
	StatusError   ConsumerStatus = "ERROR"
	StatusStopped ConsumerStatus = "STOPPED"
	StatusStalled ConsumerStatus = "STALLED"
	StatusUnknown ConsumerStatus = "UNKNOWN"
)

// OffsetRecord represents a single offset check
type OffsetRecord struct {
	Offset         int64
	HighWatermark  int64
	Lag            int64
	CheckTimestamp time.Time
}

// PartitionWindow holds the sliding window data for a partition
type PartitionWindow struct {
	Records    []OffsetRecord
	WindowSize int
	mu         sync.RWMutex
}

// PartitionStatus represents the evaluated status of a partition
type PartitionStatus struct {
	GroupID            string
	Topic              string
	Partition          int32
	Status             ConsumerStatus
	CurrentLag         int64
	LagTrend           string // "increasing", "decreasing", "stable"
	Message            string
	WindowCompleteness float64
	LastOffset         int64
	LastCheckTime      time.Time
}

// GroupStatus represents the overall status of a consumer group
type GroupStatus struct {
	GroupID           string
	OverallStatus     ConsumerStatus
	TotalLag          int64
	PartitionCount    int
	ErrorPartitions   []PartitionStatus
	WarningPartitions []PartitionStatus
}

// SlidingWindowConfig holds configuration for sliding window evaluation
type SlidingWindowConfig struct {
	WindowSize       int
	MinWindowSize    int
	StalledThreshold float64       // Percentage of unchanged offsets to consider stalled
	CheckInterval    time.Duration // How often to check offsets
}

// KafkaClient defines the interface for Kafka operations
type KafkaClient interface {
	Close()
	ListConsumerGroups(ctx context.Context) ([]string, error)
	GetConsumerGroupOffsets(ctx context.Context, groupID string) ([]kafkaclient.TopicPartitionOffset, error)
	GetHighWatermark(topic string, partition int32) (int64, error)
	GetConsumerGroupMembers(ctx context.Context, groupIDs []string) (map[string]int, error)
}

type Collector struct {
	client        KafkaClient
	config        SlidingWindowConfig
	windows       map[string]*PartitionWindow // key: "group:topic:partition"
	windowsMu     sync.RWMutex
	evaluator     *LagEvaluator
	statusStore   map[string]GroupStatus // Store latest group status
	statusStoreMu sync.RWMutex
}

// NewWithSlidingWindow creates a new collector with sliding window support
func NewWithSlidingWindow(brokers string, config SlidingWindowConfig) (*Collector, error) {
	client, err := kafkaclient.NewClient(&kafkaclient.Config{
		Brokers:        brokers,
		ConsumerGroup:  "lagradar_metrics_collector",
		RequestTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	return &Collector{
		client:      client,
		config:      config,
		windows:     make(map[string]*PartitionWindow),
		evaluator:   NewLagEvaluator(config),
		statusStore: make(map[string]GroupStatus),
	}, nil
}

// NewWithClient creates a new collector with a custom client - For Test
func NewWithClient(client KafkaClient, config SlidingWindowConfig) *Collector {
	return &Collector{
		client:      client,
		config:      config,
		windows:     make(map[string]*PartitionWindow),
		evaluator:   NewLagEvaluator(config),
		statusStore: make(map[string]GroupStatus),
	}
}

func (c *Collector) Close() {
	if c.client != nil {
		c.client.Close()
	}
}

// CollectMetrics collects metrics and evaluates consumer status
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

	// Collect metrics and evaluate status for each group
	for _, groupID := range groupIDs {
		groupStatus, err := c.collectAndEvaluateGroup(ctx, groupID)
		if err != nil {
			log.Printf("Error collecting metrics for group %s: %v", groupID, err)
			metrics.ScrapeErrors.Inc()
			continue
		}

		// Store the latest status
		c.statusStoreMu.Lock()
		c.statusStore[groupID] = groupStatus
		c.statusStoreMu.Unlock()

		c.updateStatusMetrics(groupStatus)
	}

	return nil
}

// collectAndEvaluateGroup collects metrics and evaluates status for a consumer group
func (c *Collector) collectAndEvaluateGroup(ctx context.Context, groupID string) (GroupStatus, error) {
	offsets, err := c.client.GetConsumerGroupOffsets(ctx, groupID)
	if err != nil {
		return GroupStatus{}, fmt.Errorf("failed to get group offsets: %w", err)
	}

	var partitionStatuses []PartitionStatus
	var totalLag int64
	checkTime := time.Now()

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

		// Calculate lag
		lag := highWatermark - int64(tpo.Offset)
		if lag < 0 {
			lag = 0
		}
		totalLag += lag

		// Update lag metric
		metrics.ConsumerLag.WithLabelValues(groupID, tpo.Topic, partitionStr).Set(float64(lag))

		// Add to sliding window
		windowKey := fmt.Sprintf("%s:%s:%d", groupID, tpo.Topic, tpo.Partition)
		c.addToWindow(windowKey, OffsetRecord{
			Offset:         int64(tpo.Offset),
			HighWatermark:  highWatermark,
			Lag:            lag,
			CheckTimestamp: checkTime,
		})

		// Evaluate partition status
		partitionStatus := c.evaluatePartition(windowKey, groupID, tpo.Topic, tpo.Partition)
		partitionStatuses = append(partitionStatuses, partitionStatus)

		// Update partition-level metrics
		c.updatePartitionMetrics(partitionStatus)
	}

	// Determine overall group status
	return c.determineGroupStatus(groupID, partitionStatuses, totalLag), nil
}

// addToWindow adds a record to the sliding window
func (c *Collector) addToWindow(key string, record OffsetRecord) {
	c.windowsMu.Lock()
	defer c.windowsMu.Unlock()

	window, exists := c.windows[key]
	if !exists {
		window = &PartitionWindow{
			WindowSize: c.config.WindowSize,
			Records:    make([]OffsetRecord, 0, c.config.WindowSize),
		}
		c.windows[key] = window
	}

	window.mu.Lock()
	defer window.mu.Unlock()

	window.Records = append(window.Records, record)
	if len(window.Records) > window.WindowSize {
		window.Records = window.Records[1:]
	}
}

// evaluatePartition evaluates the status of a partition based on its window
func (c *Collector) evaluatePartition(windowKey, groupID, topic string, partition int32) PartitionStatus {
	c.windowsMu.RLock()
	window, exists := c.windows[windowKey]
	c.windowsMu.RUnlock()

	if !exists || window == nil {
		return PartitionStatus{
			GroupID:   groupID,
			Topic:     topic,
			Partition: partition,
			Status:    StatusUnknown,
			Message:   "No data available",
		}
	}

	window.mu.RLock()
	records := make([]OffsetRecord, len(window.Records))
	copy(records, window.Records)
	window.mu.RUnlock()

	return c.evaluator.EvaluatePartition(records, groupID, topic, partition)
}

// determineGroupStatus determines the overall status of a consumer group
func (c *Collector) determineGroupStatus(groupID string, partitions []PartitionStatus, totalLag int64) GroupStatus {
	status := GroupStatus{
		GroupID:        groupID,
		TotalLag:       totalLag,
		PartitionCount: len(partitions),
	}

	for _, p := range partitions {
		switch p.Status {
		case StatusError, StatusStopped, StatusStalled:
			status.ErrorPartitions = append(status.ErrorPartitions, p)
		case StatusWarning:
			status.WarningPartitions = append(status.WarningPartitions, p)
		}
	}

	// Determine overall status
	if len(status.ErrorPartitions) > 0 {
		status.OverallStatus = StatusError
	} else if len(status.WarningPartitions) > 0 {
		status.OverallStatus = StatusWarning
	} else {
		status.OverallStatus = StatusOK
	}

	return status
}

// updateStatusMetrics updates Prometheus metrics based on group status
func (c *Collector) updateStatusMetrics(status GroupStatus) {
	statusValue := 0.0
	switch status.OverallStatus {
	case StatusWarning:
		statusValue = 1.0
	case StatusError, StatusStopped, StatusStalled:
		statusValue = 2.0
	}

	metrics.ConsumerGroupStatus.WithLabelValues(status.GroupID).Set(statusValue)
	metrics.ConsumerGroupErrorPartitions.WithLabelValues(status.GroupID).Set(float64(len(status.ErrorPartitions)))
	metrics.ConsumerGroupWarningPartitions.WithLabelValues(status.GroupID).Set(float64(len(status.WarningPartitions)))
}

// updatePartitionMetrics updates metrics for individual partitions
func (c *Collector) updatePartitionMetrics(partitionStatus PartitionStatus) {
	partitionStr := strconv.Itoa(int(partitionStatus.Partition))

	statusValue := statusToFloat(string(partitionStatus.Status))
	metrics.ConsumerPartitionStatus.WithLabelValues(
		partitionStatus.GroupID,
		partitionStatus.Topic,
		partitionStr,
	).Set(statusValue)

	trendValue := lagTrendToFloat(partitionStatus.LagTrend)
	metrics.ConsumerPartitionLagTrend.WithLabelValues(
		partitionStatus.GroupID,
		partitionStatus.Topic,
		partitionStr,
	).Set(trendValue)
}

func statusToFloat(status string) float64 {
	switch status {
	case "OK":
		return 0
	case "WARNING":
		return 1
	case "ERROR":
		return 2
	case "STOPPED":
		return 3
	case "STALLED":
		return 4
	default:
		return -1
	}
}

func lagTrendToFloat(trend string) float64 {
	switch trend {
	case "unknown":
		return 0
	case "stable":
		return 1
	case "increasing":
		return 2
	case "decreasing":
		return 3
	case "stopped", "stalled":
		return 4
	default:
		return 0
	}
}

// GetGroupStatus returns the latest status for a consumer group
func (c *Collector) GetGroupStatus(groupID string) (GroupStatus, bool) {
	c.statusStoreMu.RLock()
	defer c.statusStoreMu.RUnlock()
	status, exists := c.statusStore[groupID]
	return status, exists
}

// GetAllGroupStatuses returns the latest status for all consumer groups
func (c *Collector) GetAllGroupStatuses() map[string]GroupStatus {
	c.statusStoreMu.RLock()
	defer c.statusStoreMu.RUnlock()

	result := make(map[string]GroupStatus)
	for k, v := range c.statusStore {
		result[k] = v
	}
	return result
}

// StartPeriodicCollection starts periodic metric collection
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

// LagEvaluator evaluates consumer lag patterns
type LagEvaluator struct {
	config SlidingWindowConfig
}

func NewLagEvaluator(config SlidingWindowConfig) *LagEvaluator {
	return &LagEvaluator{config: config}
}

// EvaluatePartition evaluates the status of a partition based on its sliding window
func (e *LagEvaluator) EvaluatePartition(records []OffsetRecord, groupID, topic string, partition int32) PartitionStatus {
	if len(records) == 0 {
		return PartitionStatus{
			GroupID:   groupID,
			Topic:     topic,
			Partition: partition,
			Status:    StatusUnknown,
			Message:   "No data available",
		}
	}

	windowCompleteness := float64(len(records)) / float64(e.config.WindowSize) * 100
	latest := records[len(records)-1]

	// Not enough data for evaluation
	if len(records) < e.config.MinWindowSize {
		return PartitionStatus{
			GroupID:            groupID,
			Topic:              topic,
			Partition:          partition,
			Status:             StatusUnknown,
			CurrentLag:         latest.Lag,
			LagTrend:           "unknown",
			Message:            fmt.Sprintf("Insufficient data: %d/%d records", len(records), e.config.WindowSize),
			WindowCompleteness: windowCompleteness,
			LastOffset:         latest.Offset,
			LastCheckTime:      latest.CheckTimestamp,
		}
	}

	// Check if consumer is stopped
	if e.isConsumerStopped(records) {
		return PartitionStatus{
			GroupID:            groupID,
			Topic:              topic,
			Partition:          partition,
			Status:             StatusStopped,
			CurrentLag:         latest.Lag,
			LagTrend:           "stopped",
			Message:            "Consumer has stopped processing messages",
			WindowCompleteness: windowCompleteness,
			LastOffset:         latest.Offset,
			LastCheckTime:      latest.CheckTimestamp,
		}
	}

	// Check if consumer is stalled
	if e.isConsumerStalled(records) {
		return PartitionStatus{
			GroupID:            groupID,
			Topic:              topic,
			Partition:          partition,
			Status:             StatusStalled,
			CurrentLag:         latest.Lag,
			LagTrend:           "stalled",
			Message:            "Consumer is not making progress",
			WindowCompleteness: windowCompleteness,
			LastOffset:         latest.Offset,
			LastCheckTime:      latest.CheckTimestamp,
		}
	}

	// Check if consumer caught up - lag was 0
	if e.hasZeroLag(records) {
		return PartitionStatus{
			GroupID:            groupID,
			Topic:              topic,
			Partition:          partition,
			Status:             StatusOK,
			CurrentLag:         latest.Lag,
			LagTrend:           e.getLagTrend(records),
			Message:            "Consumer caught up during window",
			WindowCompleteness: windowCompleteness,
			LastOffset:         latest.Offset,
			LastCheckTime:      latest.CheckTimestamp,
		}
	}

	// Analyze lag trend
	lagTrend := e.getLagTrend(records)
	var status ConsumerStatus
	var message string

	switch lagTrend {
	case "decreasing":
		status = StatusOK
		message = "Consumer is catching up"
	case "increasing":
		if e.isConsistentlyIncreasing(records) {
			status = StatusWarning
			message = "Lag is consistently increasing"
		} else {
			status = StatusOK
			message = "Lag is increasing but consumer is making progress"
		}
	default:
		status = StatusOK
		message = "Lag is stable"
	}

	return PartitionStatus{
		GroupID:            groupID,
		Topic:              topic,
		Partition:          partition,
		Status:             status,
		CurrentLag:         latest.Lag,
		LagTrend:           lagTrend,
		Message:            message,
		WindowCompleteness: windowCompleteness,
		LastOffset:         latest.Offset,
		LastCheckTime:      latest.CheckTimestamp,
	}
}

// isConsumerStopped checks if the consumer has stopped processing
func (e *LagEvaluator) isConsumerStopped(records []OffsetRecord) bool {
	if len(records) < 2 {
		return false
	}

	uniqueOffsets := make(map[int64]bool)
	for _, r := range records {
		uniqueOffsets[r.Offset] = true
	}

	// If all offsets are the same and there's lag, consumer is stopped
	if len(uniqueOffsets) == 1 && records[len(records)-1].Lag > 0 {
		return true
	}

	// Check recent half records - TODO: Make this configurable?
	halfWindow := len(records) / 2
	recentRecords := records[halfWindow:]
	recentUniqueOffsets := make(map[int64]bool)
	for _, r := range recentRecords {
		recentUniqueOffsets[r.Offset] = true
	}

	return len(recentUniqueOffsets) == 1 && records[len(records)-1].Lag > 0
}

// isConsumerStalled checks if the consumer is stalled
func (e *LagEvaluator) isConsumerStalled(records []OffsetRecord) bool {
	if len(records) < 3 {
		return false
	}

	uniqueOffsets := make(map[int64]bool)
	for _, r := range records {
		uniqueOffsets[r.Offset] = true
	}

	uniqueRatio := float64(len(uniqueOffsets)) / float64(len(records))

	return uniqueRatio <= e.config.StalledThreshold && records[len(records)-1].Lag > 0
}

func (e *LagEvaluator) hasZeroLag(records []OffsetRecord) bool {
	for _, r := range records {
		if r.Lag == 0 {
			return true
		}
	}
	return false
}

func (e *LagEvaluator) getLagTrend(records []OffsetRecord) string {
	if len(records) < 2 {
		return "unknown"
	}

	startLag := records[0].Lag
	endLag := records[len(records)-1].Lag

	hasDecrease := false
	for i := 1; i < len(records); i++ {
		if records[i].Lag < records[i-1].Lag {
			hasDecrease = true
			break
		}
	}

	if hasDecrease {
		return "decreasing"
	} else if float64(endLag) > float64(startLag)*1.1 { // TODO: Make this configurable?
		return "increasing"
	}
	return "stable"
}

// isConsistentlyIncreasing checks if lag is consistently increasing
func (e *LagEvaluator) isConsistentlyIncreasing(records []OffsetRecord) bool {
	if len(records) < 3 {
		return false
	}

	increases := 0
	for i := 1; i < len(records); i++ {
		if records[i].Lag > records[i-1].Lag {
			increases++
		}
	}

	// TODO: Make this configurable? - 70% of the time lag is increasing as default.
	return float64(increases) > float64(len(records)-1)*0.7
}
