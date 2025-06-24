package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ConsumerLag is the current lag of a consumer group for a topic partition
	ConsumerLag = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_lag",
			Help: "Current lag of a consumer group for a topic partition",
		},
		[]string{
			"consumer_group",
			"topic",
			"partition",
		},
	)

	// ConsumerCurrentOffset is the current offset of a consumer group for a topic partition
	ConsumerCurrentOffset = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_current_offset",
			Help: "Current offset of a consumer group for a topic partition",
		},
		[]string{
			"consumer_group",
			"topic",
			"partition",
		},
	)

	// LogEndOffset is the log end offset of a topic partition
	LogEndOffset = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_partition_log_end_offset",
			Help: "Log end offset of a topic partition",
		},
		[]string{
			"topic",
			"partition",
		},
	)

	// ConsumerGroupMembers is the number of members in a consumer group
	ConsumerGroupMembers = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_group_members",
			Help: "Number of members in a consumer group",
		},
		[]string{
			"consumer_group",
		},
	)

	// ScrapeDuration is the duration of the last scrape in seconds
	ScrapeDuration = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "kafka_lag_exporter_scrape_duration_seconds",
			Help: "Duration of the last scrape in seconds",
		},
	)

	// ScrapeErrors is the total number of scrape errors
	ScrapeErrors = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "kafka_lag_exporter_scrape_errors_total",
			Help: "Total number of scrape errors",
		},
	)
)
