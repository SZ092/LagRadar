package rca

import (
	"LagRadar/internal/collector"
	"context"
	"fmt"
	"log"
	"time"
)

// EvaluatorWithRCA extends the LagEvaluator with RCA capabilities
type EvaluatorWithRCA struct {
	*collector.LagEvaluator
	rcaPublisher     *EventPublisher // Changed to pointer
	previousStatuses map[string]collector.ConsumerStatus
	lastEventTime    map[string]time.Time
}

// NewLagEvaluatorWithRCA creates a new evaluator with RCA support
func NewLagEvaluatorWithRCA(config collector.Config, rcaPublisher *EventPublisher) *EvaluatorWithRCA {
	return &EvaluatorWithRCA{
		LagEvaluator:     collector.NewLagEvaluator(config),
		rcaPublisher:     rcaPublisher,
		previousStatuses: make(map[string]collector.ConsumerStatus),
		lastEventTime:    make(map[string]time.Time),
	}
}

// EvaluatePartitionConsumerWithRCA evaluates and publishes RCA events
func (e *EvaluatorWithRCA) EvaluatePartitionConsumerWithRCA(
	records []collector.OffsetRecord,
	groupID, topic string,
	partition int32,
) collector.PartitionConsumerStatus {

	status := e.EvaluatePartitionConsumer(records, groupID, topic, partition)

	// Check for RCA events if publisher is enabled
	if e.rcaPublisher != nil {
		e.detectAndPublishEvents(status, records)
	}

	return status
}

func (e *EvaluatorWithRCA) detectAndPublishEvents(
	status collector.PartitionConsumerStatus,
	records []collector.OffsetRecord,
) {
	ctx := context.Background()
	key := e.getStatusKey(status)

	// Check if enough time has passed since last event (prevent spam)
	if lastTime, exists := e.lastEventTime[key]; exists {
		if time.Since(lastTime) < 5*time.Minute {
			return
		}
	}

	previousStatus, hasPrevious := e.previousStatuses[key]

	// Detect status changes and publish events
	if hasPrevious && previousStatus != status.Status {
		e.publishStatusChangeEvent(ctx, status, previousStatus)
	}

	// Detect concerning conditions regardless of status change
	e.detectCriticalConditions(ctx, status, records)

	// Update tracking
	e.previousStatuses[key] = status.Status
}

func (e *EvaluatorWithRCA) publishStatusChangeEvent(
	ctx context.Context,
	status collector.PartitionConsumerStatus,
	previousStatus collector.ConsumerStatus,
) {
	var eventType string
	var severity EventSeverity

	switch status.Status {
	case collector.StatusStopped:
		if previousStatus == collector.StatusActive || previousStatus == collector.StatusLagging {
			eventType = EventTypeConsumerStopped
			severity = SeverityCritical
		}
	case collector.StatusStalled:
		if previousStatus == collector.StatusActive {
			eventType = EventTypeConsumerStalled
			severity = SeverityWarning
		}
	case collector.StatusActive, collector.StatusEmpty:
		if previousStatus == collector.StatusStopped || previousStatus == collector.StatusStalled {
			eventType = EventTypeConsumerRecovered
			severity = SeverityInfo
		}
	}

	if eventType != "" {
		if err := e.rcaPublisher.PublishPartitionEvent(
			ctx, eventType, severity, status, status.Message,
		); err != nil {
			log.Printf("Failed to publish RCA event: %v", err)
		} else {
			e.lastEventTime[e.getStatusKey(status)] = time.Now()
		}
	}
}

func (e *EvaluatorWithRCA) detectCriticalConditions(
	ctx context.Context,
	status collector.PartitionConsumerStatus,
	records []collector.OffsetRecord,
) {
	// Rapid lag increase detection
	if status.LagChangeRate > e.LagEvaluator.GetConfig().RapidLagIncreaseRate*2 && status.CurrentLag > 1000 {
		message := fmt.Sprintf(
			"Lag increasing rapidly at %.0f msg/s (threshold: %.0f msg/s)",
			status.LagChangeRate,
			e.LagEvaluator.GetConfig().RapidLagIncreaseRate,
		)

		if err := e.rcaPublisher.PublishPartitionEvent(
			ctx,
			EventTypeRapidLagIncrease,
			SeverityWarning,
			status,
			message,
		); err != nil {
			log.Printf("Failed to publish rapid lag increase event: %v", err)
		} else {
			e.lastEventTime[e.getStatusKey(status)] = time.Now()
		}
	}

	// Lag cleared detection
	if status.CurrentLag == 0 && len(records) > 2 && records[len(records)-2].Lag > 10000 {
		message := "Consumer has cleared significant lag"

		if err := e.rcaPublisher.PublishPartitionEvent(
			ctx,
			EventTypeLagCleared,
			SeverityInfo,
			status,
			message,
		); err != nil {
			log.Printf("Failed to publish lag cleared event: %v", err)
		}
	}
}

func (e *EvaluatorWithRCA) getStatusKey(status collector.PartitionConsumerStatus) string {
	return fmt.Sprintf("%s:%s:%d", status.GroupID, status.Topic, status.Partition)
}
