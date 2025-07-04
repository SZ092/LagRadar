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

// CollectMetricsWithCluster CollectMetrics collects metrics and evaluates consumer status
func (c *Collector) CollectMetricsWithCluster(ctx context.Context, clusterName string) error {
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		metrics.ScrapeDuration.WithLabelValues(clusterName).Set(duration)
	}()

	groupIDs, err := c.client.ListConsumerGroups(ctx)
	if err != nil {
		metrics.ScrapeErrors.WithLabelValues(clusterName).Inc()
		return fmt.Errorf("[%s] Error: failed to list consumer groups: %v", clusterName, err)
	}

	log.Printf("[%s] Found %d consumer groups", clusterName, len(groupIDs))

	metrics.ActiveConsumerGroups.WithLabelValues(clusterName).Set(float64(len(groupIDs)))

	memberCounts, err := c.client.GetConsumerGroupMembers(ctx, groupIDs)
	if err != nil {
		log.Printf("[%s] Warning: failed to get group members: %v", clusterName, err)
		memberCounts = make(map[string]int)
	}

	// Update member count metrics
	for groupID, count := range memberCounts {
		metrics.ConsumerGroupMembers.WithLabelValues(clusterName, groupID).Set(float64(count))
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

			// Measure collection duration per group
			start := time.Now()
			groupStatus, err := c.collectGroupMetricsWithCluster(ctx, gid, memberCounts[gid], clusterName)
			metrics.CollectionDuration.WithLabelValues(clusterName, gid).Observe(time.Since(start).Seconds())
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
			log.Printf("[%s] Error collecting metrics for group %s: %v", clusterName, result.groupID, result.err)
			metrics.ScrapeErrors.WithLabelValues(clusterName).Inc()
			continue
		}

		c.statusStoreMu.Lock()
		c.statusStore[result.groupID] = result.status
		c.statusStoreMu.Unlock()

		c.updateMetricsWithCluster(result.status, clusterName)
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
func (c *Collector) collectGroupMetricsWithCluster(ctx context.Context, groupID string,
	memberCount int, clusterName string) (GroupStatus, error) {
	offsets, err := c.client.GetConsumerGroupOffsets(ctx, groupID)
	if err != nil {
		return GroupStatus{}, fmt.Errorf("[%s] Error : failed to get group offsets: %w", clusterName, err)

	}

	groupStatus := GroupStatus{
		GroupID:        groupID,
		MemberCount:    memberCount,
		LastUpdateTime: time.Now(),
		PartitionCount: len(offsets),
	}

	var totalLag int64

	for _, tpo := range offsets {
		partitionStatus, err := c.evaluatePartition(ctx, groupID, tpo, clusterName)
		if err != nil {
			log.Printf("[%s] Error: Failed to evaluate partition %s[%d] for group %s: %v", clusterName,
				tpo.Topic, tpo.Partition, groupID, err)
			continue
		}

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
func (c *Collector) evaluatePartition(ctx context.Context, groupID string, tpo kafkaclient.TopicPartitionOffset,
	clusterName string) (PartitionConsumerStatus, error) {

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
	c.updatePartitionMetricsWithCluster(status, clusterName)

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

// updateMetricsWithCluster updates Prometheus metrics with cluster label
func (c *Collector) updateMetricsWithCluster(status GroupStatus, clusterName string) {
	healthValue := healthToFloat(status.OverallHealth)
	metrics.ConsumerGroupHealth.WithLabelValues(clusterName, status.GroupID).Set(healthValue)
	metrics.ConsumerGroupTotalLag.WithLabelValues(clusterName, status.GroupID).Set(float64(status.TotalLag))
	metrics.ConsumerGroupActivePartitions.WithLabelValues(clusterName, status.GroupID).Set(float64(status.ActivePartitions))
	metrics.ConsumerGroupStoppedPartitions.WithLabelValues(clusterName, status.GroupID).Set(float64(status.StoppedPartitions))
	metrics.ConsumerGroupStalledPartitions.WithLabelValues(clusterName, status.GroupID).Set(float64(status.StalledPartitions))
}

// updatePartitionMetrics updates partition-level metrics
func (c *Collector) updatePartitionMetricsWithCluster(status PartitionConsumerStatus, clusterName string) {
	partitionStr := strconv.Itoa(int(status.Partition))
	labels := []string{clusterName, status.GroupID, status.Topic, partitionStr}

	metrics.ConsumerCurrentOffset.WithLabelValues(labels...).Set(float64(status.CurrentOffset))
	metrics.ConsumerLag.WithLabelValues(labels...).Set(float64(status.CurrentLag))
	metrics.LogEndOffset.WithLabelValues(clusterName, status.Topic, partitionStr).Set(float64(status.HighWatermark))

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
func (c *Collector) StartPeriodicCollection(ctx context.Context, clusterName string) {
	ticker := time.NewTicker(c.config.CheckInterval)
	cleanupTicker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	defer cleanupTicker.Stop()

	if err := c.CollectMetricsWithCluster(ctx, clusterName); err != nil {
		log.Printf("Initial collection error: %v", err)
	}
	for {
		select {
		case <-cleanupTicker.C:
			activeGroups := make(map[string]bool)
			for groupID := range c.GetAllGroupStatuses() {
				activeGroups[groupID] = true
			}
			c.CleanupOldWindows(activeGroups)
		case <-ctx.Done():
			log.Println("Stopping periodic collection")
			return
		case <-ticker.C:
			if err := c.CollectMetricsWithCluster(ctx, clusterName); err != nil {
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
