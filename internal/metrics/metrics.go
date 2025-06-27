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

// New metrics for sliding window
var (
	// ConsumerGroupStatus represents the overall health of a consumer group
	ConsumerGroupStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_group_status",
			Help: "Status of consumer group (0=OK, 1=WARNING, 2=ERROR)",
		},
		[]string{"consumer_group"},
	)

	// ConsumerGroupErrorPartitions is the number of partitions in error state
	ConsumerGroupErrorPartitions = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_group_error_partitions",
			Help: "Number of partitions in error state",
		},
		[]string{"consumer_group"},
	)

	// ConsumerGroupWarningPartitions is the number of partitions in warning state
	ConsumerGroupWarningPartitions = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_group_warning_partitions",
			Help: "Number of partitions in warning state",
		},
		[]string{"consumer_group"},
	)

	// ConsumerPartitionStatus represents the status of a specific partition
	ConsumerPartitionStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_partition_status",
			Help: "Status of consumer partition (0=OK, 1=WARNING, 2=ERROR, 3=STOPPED, 4=STALLED, -1=UNKNOWN)",
		},
		[]string{"consumer_group", "topic", "partition"},
	)

	// ConsumerPartitionLagTrend represents the lag trend for a partition
	ConsumerPartitionLagTrend = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_partition_lag_trend",
			Help: "Lag trend for partition (0=unknown, 1=stable, 2=increasing, 3=decreasing, 4=stopped/stalled)",
		},
		[]string{"consumer_group", "topic", "partition"},
	)
)
