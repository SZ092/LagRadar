package redis

import (
	"time"
)

// EventSource identifies the source system
type EventSource string

const (
	SourceLagRadar EventSource = "lagradar"
)

// EventSeverity defines the severity level
type EventSeverity string

const (
	SeverityInfo     EventSeverity = "info"
	SeverityWarning  EventSeverity = "warning"
	SeverityCritical EventSeverity = "critical"
)

// BaseEvent is the common structure for all events
type BaseEvent struct {
	// Metadata
	ID        string        `json:"id"`
	Source    EventSource   `json:"source"`
	Type      string        `json:"type"`
	Severity  EventSeverity `json:"severity"`
	Timestamp time.Time     `json:"timestamp"`
	Cluster   string        `json:"cluster,omitempty"`
	Namespace string        `json:"namespace,omitempty"`
	Service   string        `json:"service,omitempty"`

	// Event Data
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Data        map[string]interface{} `json:"data"`

	Fingerprint string `json:"fingerprint"`
}

// KafkaLagEvent represents Kafka consumer lag events from LagRadar
type KafkaLagEvent struct {
	BaseEvent

	// Kafka specific fields
	ConsumerGroup string `json:"consumer_group"`
	Topic         string `json:"topic"`
	Partition     int32  `json:"partition"`

	// Metrics - snapshot
	CurrentLag        int64   `json:"current_lag"`
	CurrentOffset     int64   `json:"current_offset"`
	HighWatermark     int64   `json:"high_watermark"`
	LagDelta          int64   `json:"lag_delta"`
	OffsetDelta       int64   `json:"offset_delta"`
	ConsumptionRate   float64 `json:"consumption_rate"`
	LagChangeRate     float64 `json:"lag_change_rate"`
	TimeSinceLastMove string  `json:"time_since_last_move"`
}

// Event types
const (
	EventTypeConsumerStopped   = "consumer_stopped"
	EventTypeConsumerStalled   = "consumer_stalled"
	EventTypeRapidLagIncrease  = "rapid_lag_increase"
	EventTypeConsumerRecovered = "consumer_recovered"
	EventTypeLagCleared        = "lag_cleared"
)
