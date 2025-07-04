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
			"cluster",
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
			"cluster",
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
			"cluster",
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
			"cluster",
			"consumer_group",
		},
	)

	// ScrapeDuration is the duration of the last scrape in seconds
	ScrapeDuration = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_lag_exporter_scrape_duration_seconds",
			Help: "Duration of the last scrape in seconds",
		},
		[]string{"cluster"},
	)

	// ScrapeErrors is the total number of scrape errors
	ScrapeErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_lag_exporter_scrape_errors_total",
			Help: "Total number of scrape errors",
		},
		[]string{"cluster"},
	)
)

// Metrics for sliding window detection
var (
	ConsumerGroupHealth = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_group_health",
			Help: "Overall health of consumer group (0=good, 1=warning, 2=critical)",
		},
		[]string{"cluster", "group"},
	)

	ConsumerGroupTotalLag = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_group_total_lag",
			Help: "Total lag across all partitions for a consumer group",
		},
		[]string{"cluster", "group"},
	)

	ConsumerGroupActivePartitions = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_group_active_partitions",
			Help: "Number of actively consuming partitions",
		},
		[]string{"cluster", "group"},
	)

	ConsumerGroupStoppedPartitions = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_group_stopped_partitions",
			Help: "Number of stopped partitions",
		},
		[]string{"cluster", "group"},
	)

	ConsumerGroupStalledPartitions = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_group_stalled_partitions",
			Help: "Number of stalled partitions",
		},
		[]string{"cluster", "group"},
	)

	// Consumer State Metrics
	ConsumerStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_status",
			Help: "Consumer operational status (0=active, 1=lagging, 2=stalled, 3=stopped, 4=empty, -1=unknown)",
		},
		[]string{"cluster", "group", "topic", "partition"},
	)

	ConsumerLagTrend = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_lag_trend",
			Help: "Consumer lag trend (0=unknown, 1=stable, 2=increasing, 3=decreasing)",
		},
		[]string{"cluster", "group", "topic", "partition"},
	)

	ConsumerHealth = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_health",
			Help: "Consumer health status (0=good, 1=warning, 2=critical)",
		},
		[]string{"cluster", "group", "topic", "partition"},
	)

	// Performance Metrics
	ConsumerConsumptionRate = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_consumption_rate",
			Help: "Consumer message consumption rate (messages/sec)",
		},
		[]string{"cluster", "group", "topic", "partition"},
	)

	ConsumerLagChangeRate = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_lag_change_rate",
			Help: "Rate of lag change (positive=increasing, negative=decreasing, messages/sec)",
		},
		[]string{"cluster", "group", "topic", "partition"},
	)

	ConsumerTimeSinceLastActivity = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_time_since_last_activity_seconds",
			Help: "Time since consumer last moved offset (seconds)",
		},
		[]string{"cluster", "group", "topic", "partition"},
	)

	ConsumerLastActivityTimestamp = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_last_activity_timestamp",
			Help: "Unix timestamp of last consumer activity",
		},
		[]string{"cluster", "group", "topic", "partition"},
	)
)

// TODO: Implement monitoring stack for LagRadar
var (
	CollectionDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "lagradar_collection_duration_seconds",
			Help:    "Time spent collecting metrics per consumer group",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"cluster", "consumer_group"},
	)

	ActiveConsumerGroups = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "lagradar_active_consumer_groups",
			Help: "Number of active consumer groups being monitored",
		},
		[]string{"cluster"},
	)

	ClusterStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "lagradar_cluster_status",
			Help: "Status of Kafka cluster connection (0=healthy, 1=error)",
		},
		[]string{"cluster"},
	)
)
