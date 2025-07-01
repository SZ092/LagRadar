package collector

import (
	"fmt"
	"math"
	"time"
)

// LagEvaluator evaluates consumer lag patterns
type LagEvaluator struct {
	config Config
}

// NewLagEvaluator creates a new lag evaluator
func NewLagEvaluator(config Config) *LagEvaluator {
	return &LagEvaluator{config: config}
}

// EvaluatePartitionConsumer evaluates the status of a partition consumer
func (e *LagEvaluator) EvaluatePartitionConsumer(records []OffsetRecord, groupID, topic string, partition int32) PartitionConsumerStatus {
	if len(records) == 0 {
		return PartitionConsumerStatus{
			GroupID:   groupID,
			Topic:     topic,
			Partition: partition,
			Status:    StatusUnknown,
			LagTrend:  TrendUnknown,
			Health:    HealthWarning,
			Message:   "No data available",
		}
	}

	latest := records[len(records)-1]
	result := PartitionConsumerStatus{
		GroupID:            groupID,
		Topic:              topic,
		Partition:          partition,
		CurrentOffset:      latest.Offset,
		HighWatermark:      latest.HighWatermark,
		CurrentLag:         latest.Lag,
		LastUpdateTime:     latest.CheckTimestamp,
		WindowCompleteness: float64(len(records)) / float64(e.config.WindowSize) * 100,
	}

	// Not enough data for meaningful evaluation
	if len(records) < e.config.MinWindowSize {
		result.Status = StatusUnknown
		result.LagTrend = TrendUnknown
		result.Health = HealthWarning
		result.Message = fmt.Sprintf("Insufficient data: %d/%d records", len(records), e.config.MinWindowSize)
		return result
	}

	e.calculateMetrics(&result, records)

	e.determineStatus(&result, records)
	e.determineTrend(&result, records)
	e.determineHealth(&result)

	return result
}

// calculateMetrics calculates all metrics for the partition consumer
func (e *LagEvaluator) calculateMetrics(result *PartitionConsumerStatus, records []OffsetRecord) {

	result.IsActive = e.isConsumerActive(records)
	result.TimeSinceLastMove = e.timeSinceLastOffsetChange(records)

	result.ConsumptionRate = e.calculateConsumptionRate(records)
	result.LagChangeRate = e.calculateLagChangeRate(records)
}

// determineStatus determines the operational status of the consumer
func (e *LagEvaluator) determineStatus(result *PartitionConsumerStatus, records []OffsetRecord) {

	if !result.IsActive {
		if result.CurrentLag == 0 {
			result.Status = StatusEmpty
			result.Message = "Consumer is caught up and idle"
		} else if result.TimeSinceLastMove > e.config.InactivityTimeout {
			result.Status = StatusStopped
			result.Message = fmt.Sprintf("Consumer stopped for %v with lag %d",
				result.TimeSinceLastMove.Round(time.Second), result.CurrentLag)
		} else {
			result.Status = StatusStalled
			result.Message = fmt.Sprintf("Consumer stalled for %v",
				result.TimeSinceLastMove.Round(time.Second))
		}
		return
	}

	if result.ConsumptionRate < e.config.StalledConsumptionRate {
		result.Status = StatusStalled
		result.Message = fmt.Sprintf("Very slow consumption: %.2f msg/s", result.ConsumptionRate)
		return
	}

	if result.CurrentLag == 0 {
		result.Status = StatusEmpty
		result.Message = "Consumer is actively processing and caught up"
		return
	}

	if result.LagChangeRate > e.config.RapidLagIncreaseRate {
		result.Status = StatusLagging
		result.Message = fmt.Sprintf("Lag increasing rapidly at %.2f msg/s", result.LagChangeRate)
		return
	}

	result.Status = StatusActive
	result.Message = fmt.Sprintf("Processing at %.2f msg/s with lag %d",
		result.ConsumptionRate, result.CurrentLag)
}

// determineTrend determines the lag trend
func (e *LagEvaluator) determineTrend(result *PartitionConsumerStatus, records []OffsetRecord) {
	if len(records) < 3 {
		result.LagTrend = TrendUnknown
		return
	}

	// Calculate trend using linear regression or simple comparison
	trend := e.calculateLagTrend(records)
	result.LagTrend = trend
}

// determineHealth determines the overall health based on status and metrics
func (e *LagEvaluator) determineHealth(result *PartitionConsumerStatus) {
	switch result.Status {
	case StatusStopped, StatusStalled:
		result.Health = HealthCritical
	case StatusLagging:
		if result.LagTrend == TrendIncreasing && result.LagChangeRate > e.config.RapidLagIncreaseRate {
			result.Health = HealthCritical
		} else {
			result.Health = HealthWarning
		}
	case StatusEmpty, StatusActive:
		if result.LagTrend == TrendIncreasing && result.CurrentLag > 1000 {
			result.Health = HealthWarning
		} else {
			result.Health = HealthGood
		}
	default:
		result.Health = HealthWarning
	}
}

// isConsumerActive checks if the consumer has been active recently
func (e *LagEvaluator) isConsumerActive(records []OffsetRecord) bool {
	if len(records) < 2 {
		return false
	}

	// Check recent records for offset changes
	recentCount := min(5, len(records))
	uniqueOffsets := make(map[int64]bool)

	for i := len(records) - recentCount; i < len(records); i++ {
		uniqueOffsets[records[i].Offset] = true
	}

	return len(uniqueOffsets) > 1
}

// timeSinceLastOffsetChange calculates time since last offset change
func (e *LagEvaluator) timeSinceLastOffsetChange(records []OffsetRecord) time.Duration {
	if len(records) < 2 {
		return 0
	}

	lastOffset := records[len(records)-1].Offset

	// Find the last time offset changed
	for i := len(records) - 2; i >= 0; i-- {
		if records[i].Offset != lastOffset {
			// Offset changed between record i and i+1
			return records[len(records)-1].CheckTimestamp.Sub(records[i+1].CheckTimestamp)
		}
	}

	// All records have the same offset
	return records[len(records)-1].CheckTimestamp.Sub(records[0].CheckTimestamp)
}

// calculateConsumptionRate calculates the message consumption rate
func (e *LagEvaluator) calculateConsumptionRate(records []OffsetRecord) float64 {
	if len(records) < 2 {
		return 0
	}

	startIdx := 0
	endIdx := len(records) - 1

	firstOffset := records[0].Offset
	lastOffset := records[endIdx].Offset

	// If no progress made
	if firstOffset == lastOffset {
		return 0
	}

	// Calculate rate
	offsetDiff := lastOffset - firstOffset
	timeDiff := records[endIdx].CheckTimestamp.Sub(records[startIdx].CheckTimestamp).Seconds()

	if timeDiff == 0 {
		return 0
	}

	return float64(offsetDiff) / timeDiff
}

// calculateLagChangeRate calculates the rate of lag change
func (e *LagEvaluator) calculateLagChangeRate(records []OffsetRecord) float64 {
	if len(records) < 2 {
		return 0
	}

	// Use first and last records
	first := records[0]
	last := records[len(records)-1]

	lagDiff := float64(last.Lag - first.Lag)
	timeDiff := last.CheckTimestamp.Sub(first.CheckTimestamp).Seconds()

	if timeDiff == 0 {
		return 0
	}

	return lagDiff / timeDiff
}

// calculateLagTrend determines the lag trend
func (e *LagEvaluator) calculateLagTrend(records []OffsetRecord) LagTrend {
	if len(records) < 3 {
		return TrendUnknown
	}

	// TODO: Apply linear regression for better accuracy.
	third := len(records) / 3
	if third == 0 {
		third = 1
	}

	firstThirdAvg := e.averageLag(records[:third])
	lastThirdAvg := e.averageLag(records[len(records)-third:])

	if firstThirdAvg == 0 && lastThirdAvg == 0 {
		return TrendStable
	}

	var relativeChange float64
	if firstThirdAvg > 0 {
		relativeChange = (lastThirdAvg - firstThirdAvg) / firstThirdAvg
	} else {
		if lastThirdAvg > 0 {
			return TrendIncreasing
		}
		return TrendStable
	}

	// Determine trend based on relative change and threshold
	if math.Abs(relativeChange) < e.config.LagTrendThreshold {
		return TrendStable
	} else if relativeChange > 0 {
		return TrendIncreasing
	} else {
		return TrendDecreasing
	}
}

// averageLag calculates the average lag for a set of records
func (e *LagEvaluator) averageLag(records []OffsetRecord) float64 {
	if len(records) == 0 {
		return 0
	}

	sum := int64(0)
	for _, r := range records {
		sum += r.Lag
	}

	return float64(sum) / float64(len(records))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
