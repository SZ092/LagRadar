package distributed

import (
	"LagRadar/internal/collector"
	"time"
)

// DistributedConfig represents config for distributed coordination
type DistributedConfig struct {
	Enabled    bool   `yaml:"enabled"`
	InstanceID string `yaml:"instance_id"` // auto-generate if set empty

	Redis RedisConfig `yaml:"redis"`

	// Distributed components
	Coordinator CoordinatorConfig `yaml:"coordinator"`
	StateStore  StateStoreConfig  `yaml:"state_store"`

	// Collector
	Collector collector.Config `yaml:"collector"`
}

// RedisConfig holds config for Redis connection
type RedisConfig struct {
	Addr         string        `yaml:"addr"`
	Password     string        `yaml:"password"`
	DB           int           `yaml:"db"`
	DialTimeout  time.Duration `yaml:"dial_timeout"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	PoolSize     int           `yaml:"pool_size"`
}

// CoordinatorConfig holds config for coordinator-
type CoordinatorConfig struct {
	LeaderElection LeaderElectionConfig `yaml:"leader_election"`
	Sharding       ConsistentHashConfig `yaml:"sharding"`
	InstanceWatch  InstanceWatchConfig  `yaml:"instance_watch"`
}

// LeaderElectionConfig holds config for leader election
type LeaderElectionConfig struct {
	Enabled     bool          `yaml:"enabled"`
	LockKey     string        `yaml:"lock_key"`
	LockTTL     time.Duration `yaml:"lock_ttl"`
	RenewPeriod time.Duration `yaml:"renew_period"`
	GracePeriod time.Duration `yaml:"grace_period"` // waiting period for new leader
}

// ConsistentHashConfig holds config for consistent hash sharding
type ConsistentHashConfig struct {
	RebalanceInterval time.Duration `yaml:"rebalance_interval"`
	PartitionCount    int           `yaml:"partition_count"`
	ReplicationFactor int           `yaml:"replication_factor"`
	Load              float64       `yaml:"load"`
}

// InstanceWatchConfig holds config for instance watch
type InstanceWatchConfig struct {
	Enabled       bool          `yaml:"enabled"`
	CheckInterval time.Duration `yaml:"check_interval"`
	DownThreshold time.Duration `yaml:"down_threshold"`
}

// StateStoreConfig holds config for state store
type StateStoreConfig struct {
	InstanceTTL        time.Duration `yaml:"instance_ttl"`
	InstanceTimeout    time.Duration `yaml:"instance_timeout"`
	PartitionWindowTTL time.Duration `yaml:"partition_window_ttl"`
	AssignmentKey      string        `yaml:"assignment_key"`
}

// InstanceInfo holds info for each instance
type InstanceInfo struct {
	ID            string            `json:"id"`
	LastHeartbeat time.Time         `json:"last_heartbeat"`
	Status        string            `json:"status"`
	AssignedCount int               `json:"assigned_count"`
	Metadata      map[string]string `json:"metadata"`
}
