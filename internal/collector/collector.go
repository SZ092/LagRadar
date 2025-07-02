package collector

import (
	"LagRadar/internal/metrics"
	kafkaclient "LagRadar/pkg/kafka"
	"context"
	"fmt"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ConsumerStatus represents the operational state of a consumer
type ConsumerStatus string

const (
	StatusActive  ConsumerStatus = "ACTIVE"  // Consumer is actively processing messages
	StatusLagging ConsumerStatus = "LAGGING" // Consumer is active but falling behind
	StatusStalled ConsumerStatus = "STALLED" // Consumer is making very slow progress
	StatusStopped ConsumerStatus = "STOPPED" // Consumer has completely stopped
	StatusEmpty   ConsumerStatus = "EMPTY"   // No lag, consumer is caught up
	StatusUnknown ConsumerStatus = "UNKNOWN" // Not enough data to determine
)

// LagTrend represents the direction of lag change over time
type LagTrend string

const (
	TrendIncreasing LagTrend = "INCREASING"
	TrendDecreasing LagTrend = "DECREASING"
	TrendStable     LagTrend = "STABLE"
	TrendUnknown    LagTrend = "UNKNOWN"
)

// ConsumerHealth represents the health assessment
type ConsumerHealth string

const (
	HealthGood     ConsumerHealth = "GOOD"     // Everything is working well
	HealthWarning  ConsumerHealth = "WARNING"  // Potential issues detected
	HealthCritical ConsumerHealth = "CRITICAL" // Serious issues requiring attention
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

// PartitionConsumerStatus represents the status of a specific consumer group on a partition
type PartitionConsumerStatus struct {
	GroupID        string
	Topic          string
	Partition      int32
	CurrentOffset  int64
	HighWatermark  int64
	CurrentLag     int64
	LastUpdateTime time.Time
	// Metrics
	Status             ConsumerStatus
	LagTrend           LagTrend
	Health             ConsumerHealth
	ConsumptionRate    float64
	LagChangeRate      float64
	WindowCompleteness float64
	Message            string
	IsActive           bool //  If offset changed recently
	TimeSinceLastMove  time.Duration
}

// GroupStatus represents the overall status of a consumer group
type GroupStatus struct {
	GroupID           string
	OverallHealth     ConsumerHealth
	TotalLag          int64
	PartitionCount    int
	ActivePartitions  int
	StoppedPartitions int
	StalledPartitions int
	MemberCount       int
	LastUpdateTime    time.Time

	CriticalPartitions []PartitionConsumerStatus
	WarningPartitions  []PartitionConsumerStatus
	HealthyPartitions  []PartitionConsumerStatus
}

// Config holds configuration for the collector
type Config struct {
	WindowSize    int           // Number of records to keep in window
	MinWindowSize int           // Minimum records needed for evaluation
	CheckInterval time.Duration // How often to check offsets

	StalledConsumptionRate float64       // Consumer is considered stalled if below
	RapidLagIncreaseRate   float64       // Lag increase is concerning if above
	LagTrendThreshold      float64       // Percentage change to determine trend
	InactivityTimeout      time.Duration // Time without offset change to consider stopped

	MaxConcurrency int // Max concurrent group collections
}

// KafkaClient defines the interface for Kafka operations
type KafkaClient interface {
	Close()
	ListConsumerGroups(ctx context.Context) ([]string, error)
	GetConsumerGroupOffsets(ctx context.Context, groupID string) ([]kafkaclient.TopicPartitionOffset, error)
	GetHighWatermark(topic string, partition int32) (int64, error)
	GetConsumerGroupMembers(ctx context.Context, groupIDs []string) (map[string]int, error)
}

// Collector is the main collector implementation
type Collector struct {
	client        KafkaClient
	config        Config
	windows       map[string]*PartitionWindow // key: "group:topic:partition"
	windowsMu     sync.RWMutex
	evaluator     *LagEvaluator
	statusStore   map[string]GroupStatus // Store latest group status
	statusStoreMu sync.RWMutex
	hasCollected  atomic.Bool
}

// NewWithConfig creates a new collector with custom configuration - For test
func NewWithConfig(brokers string, config Config) (*Collector, error) {
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

// NewWithClient creates a new collector with a custom client (for testing)
func NewWithClient(client KafkaClient, config Config) *Collector {
	return &Collector{
		client:      client,
		config:      config,
		windows:     make(map[string]*PartitionWindow),
		evaluator:   NewLagEvaluator(config),
		statusStore: make(map[string]GroupStatus),
	}
}

// Close closes the collector and releases resources
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

	groupIDs, err := c.client.ListConsumerGroups(ctx)
	if err != nil {
		metrics.ScrapeErrors.Inc()
		return fmt.Errorf("failed to list consumer groups: %w", err)
	}

	log.Printf("Found %d consumer groups", len(groupIDs))

	memberCounts, err := c.client.GetConsumerGroupMembers(ctx, groupIDs)
	if err != nil {
		log.Printf("Warning: failed to get group members: %v", err)
		memberCounts = make(map[string]int)
	}

	// Update member count metrics
	for groupID, count := range memberCounts {
		metrics.ConsumerGroupMembers.WithLabelValues(groupID).Set(float64(count))
	}

	sem := make(chan struct{}, c.config.MaxConcurrency)
	var wg sync.WaitGroup
	results := make(chan groupResult, len(groupIDs))

	for _, groupID := range groupIDs {
		wg.Add(1)
		sem <- struct{}{}

		go func(gid string) {
			defer wg.Done()
			defer func() { <-sem }()

			groupStatus, err := c.collectGroupMetrics(ctx, gid, memberCounts[gid])
			results <- groupResult{
				groupID: gid,
				status:  groupStatus,
				err:     err,
			}
		}(groupID)
	}

	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	for result := range results {
		if result.err != nil {
			log.Printf("Error collecting metrics for group %s: %v", result.groupID, result.err)
			metrics.ScrapeErrors.Inc()
			continue
		}

		c.statusStoreMu.Lock()
		c.statusStore[result.groupID] = result.status
		c.statusStoreMu.Unlock()

		c.updateMetrics(result.status)
		c.hasCollected.Store(true)
	}

	return nil
}

type groupResult struct {
	groupID string
	status  GroupStatus
	err     error
}

// collectGroupMetrics collects and evaluates metrics for a single consumer group
func (c *Collector) collectGroupMetrics(ctx context.Context, groupID string, memberCount int) (GroupStatus, error) {
	offsets, err := c.client.GetConsumerGroupOffsets(ctx, groupID)
	if err != nil {
		return GroupStatus{}, fmt.Errorf("failed to get group offsets: %w", err)
	}

	groupStatus := GroupStatus{
		GroupID:        groupID,
		MemberCount:    memberCount,
		LastUpdateTime: time.Now(),
		PartitionCount: len(offsets),
	}

	var totalLag int64
	partitionStatuses := make([]PartitionConsumerStatus, 0, len(offsets))

	for _, tpo := range offsets {
		partitionStatus, err := c.evaluatePartition(ctx, groupID, tpo)
		if err != nil {
			log.Printf("Failed to evaluate partition %s[%d] for group %s: %v",
				tpo.Topic, tpo.Partition, groupID, err)
			continue
		}

		partitionStatuses = append(partitionStatuses, partitionStatus)
		totalLag += partitionStatus.CurrentLag

		// Count partition states
		switch partitionStatus.Status {
		case StatusActive, StatusEmpty:
			groupStatus.ActivePartitions++
		case StatusStopped:
			groupStatus.StoppedPartitions++
		case StatusStalled:
			groupStatus.StalledPartitions++
		}

		// Group partitions by health
		switch partitionStatus.Health {
		case HealthCritical:
			groupStatus.CriticalPartitions = append(groupStatus.CriticalPartitions, partitionStatus)
		case HealthWarning:
			groupStatus.WarningPartitions = append(groupStatus.WarningPartitions, partitionStatus)
		case HealthGood:
			groupStatus.HealthyPartitions = append(groupStatus.HealthyPartitions, partitionStatus)
		}
	}

	groupStatus.TotalLag = totalLag

	// Determine overall health
	if len(groupStatus.CriticalPartitions) > 0 {
		groupStatus.OverallHealth = HealthCritical
	} else if len(groupStatus.WarningPartitions) > 0 {
		groupStatus.OverallHealth = HealthWarning
	} else {
		groupStatus.OverallHealth = HealthGood
	}

	return groupStatus, nil
}

// evaluatePartition evaluates a single partition for a consumer group
func (c *Collector) evaluatePartition(ctx context.Context, groupID string, tpo kafkaclient.TopicPartitionOffset) (PartitionConsumerStatus, error) {

	highWatermark, err := c.client.GetHighWatermark(tpo.Topic, tpo.Partition)
	if err != nil {
		return PartitionConsumerStatus{}, err
	}

	// Skip if no committed offset
	if tpo.Offset == kafka.OffsetInvalid {
		return PartitionConsumerStatus{
			GroupID:       groupID,
			Topic:         tpo.Topic,
			Partition:     tpo.Partition,
			Status:        StatusUnknown,
			Health:        HealthWarning,
			Message:       "No committed offset",
			HighWatermark: highWatermark,
		}, nil
	}

	// Calculate lag
	lag := highWatermark - int64(tpo.Offset)
	if lag < 0 {
		lag = 0
	}

	// Create offset record
	record := OffsetRecord{
		Offset:         int64(tpo.Offset),
		HighWatermark:  highWatermark,
		Lag:            lag,
		CheckTimestamp: time.Now(),
	}

	windowKey := fmt.Sprintf("%s:%s:%d", groupID, tpo.Topic, tpo.Partition)
	c.addToWindow(windowKey, record)

	records := c.getWindowRecords(windowKey)
	status := c.evaluator.EvaluatePartitionConsumer(records, groupID, tpo.Topic, tpo.Partition)
	c.updatePartitionMetrics(status)

	return status, nil
}

// addToWindow adds a record to the sliding window
func (c *Collector) addToWindow(key string, record OffsetRecord) {
	c.windowsMu.Lock()
	window, exists := c.windows[key]
	if !exists {
		window = &PartitionWindow{
			WindowSize: c.config.WindowSize,
			Records:    make([]OffsetRecord, 0, c.config.WindowSize),
		}
		c.windows[key] = window
	}
	c.windowsMu.Unlock()

	window.mu.Lock()
	defer window.mu.Unlock()

	window.Records = append(window.Records, record)
	if len(window.Records) > window.WindowSize {
		window.Records = window.Records[1:]
	}
}

// getWindowRecords returns a copy of window records
func (c *Collector) getWindowRecords(key string) []OffsetRecord {
	c.windowsMu.RLock()
	window, exists := c.windows[key]
	c.windowsMu.RUnlock()

	if !exists || window == nil {
		return nil
	}

	window.mu.RLock()
	defer window.mu.RUnlock()

	records := make([]OffsetRecord, len(window.Records))
	copy(records, window.Records)
	return records
}

// updateMetrics updates Prometheus metrics
func (c *Collector) updateMetrics(status GroupStatus) {
	healthValue := healthToFloat(status.OverallHealth)
	metrics.ConsumerGroupHealth.WithLabelValues(status.GroupID).Set(healthValue)
	metrics.ConsumerGroupTotalLag.WithLabelValues(status.GroupID).Set(float64(status.TotalLag))
	metrics.ConsumerGroupActivePartitions.WithLabelValues(status.GroupID).Set(float64(status.ActivePartitions))
	metrics.ConsumerGroupStoppedPartitions.WithLabelValues(status.GroupID).Set(float64(status.StoppedPartitions))
	metrics.ConsumerGroupStalledPartitions.WithLabelValues(status.GroupID).Set(float64(status.StalledPartitions))
}

// updatePartitionMetrics updates partition-level metrics
func (c *Collector) updatePartitionMetrics(status PartitionConsumerStatus) {
	partitionStr := strconv.Itoa(int(status.Partition))
	labels := []string{status.GroupID, status.Topic, partitionStr}

	metrics.ConsumerCurrentOffset.WithLabelValues(labels...).Set(float64(status.CurrentOffset))
	metrics.ConsumerLag.WithLabelValues(labels...).Set(float64(status.CurrentLag))
	metrics.LogEndOffset.WithLabelValues(status.Topic, partitionStr).Set(float64(status.HighWatermark))

	metrics.ConsumerStatus.WithLabelValues(labels...).Set(statusToFloat(status.Status))
	metrics.ConsumerLagTrend.WithLabelValues(labels...).Set(trendToFloat(status.LagTrend))
	metrics.ConsumerHealth.WithLabelValues(labels...).Set(healthToFloat(status.Health))

	metrics.ConsumerConsumptionRate.WithLabelValues(labels...).Set(status.ConsumptionRate)
	metrics.ConsumerLagChangeRate.WithLabelValues(labels...).Set(status.LagChangeRate)
	metrics.ConsumerTimeSinceLastActivity.WithLabelValues(labels...).Set(status.TimeSinceLastMove.Seconds())

	if status.IsActive {
		metrics.ConsumerLastActivityTimestamp.WithLabelValues(labels...).Set(float64(status.LastUpdateTime.Unix()))
	}
}

func statusToFloat(status ConsumerStatus) float64 {
	switch status {
	case StatusActive:
		return 0
	case StatusLagging:
		return 1
	case StatusStalled:
		return 2
	case StatusStopped:
		return 3
	case StatusEmpty:
		return 4
	default:
		return -1
	}
}

func trendToFloat(trend LagTrend) float64 {
	switch trend {
	case TrendStable:
		return 1
	case TrendIncreasing:
		return 2
	case TrendDecreasing:
		return 3
	default:
		return 0
	}
}

func healthToFloat(health ConsumerHealth) float64 {
	switch health {
	case HealthGood:
		return 0
	case HealthWarning:
		return 1
	case HealthCritical:
		return 2
	default:
		return -1
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
func (c *Collector) StartPeriodicCollection(ctx context.Context) {
	ticker := time.NewTicker(c.config.CheckInterval)
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

// IsReady expose if LagRadar has collected metric data at least once
func (c *Collector) IsReady() bool {
	return c.hasCollected.Load()
}

// CleanupOldWindows removes windows for groups that no longer exist
func (c *Collector) CleanupOldWindows(activeGroups map[string]bool) {
	c.windowsMu.Lock()
	defer c.windowsMu.Unlock()

	for key := range c.windows {
		groupID := key[:strings.Index(key, ":")]
		if !activeGroups[groupID] {
			delete(c.windows, key)
		}
	}
}
