package coordinator

import (
	"github.com/buraksezer/consistent"
	"github.com/cespare/xxhash/v2"
)

// ShardingStrategy defines the sharding interface.
type ShardingStrategy interface {
	AssignGroups(groups []string, instances []string) map[string][]string
}

// ConsistentHashConfig holds configuration for consistent hash sharding
type ConsistentHashConfig struct {
	PartitionCount    int     `yaml:"partition_count"`
	ReplicationFactor int     `yaml:"replication_factor"`
	Load              float64 `yaml:"load"`
}

// DefaultConsistentHashConfig returns default configuration
func DefaultConsistentHashConfig() ConsistentHashConfig {
	return ConsistentHashConfig{
		PartitionCount:    997,
		ReplicationFactor: 20,
		Load:              1.25,
	}
}

// ConsistentHashSharding implements consistent hash based sharding
type ConsistentHashSharding struct {
	config ConsistentHashConfig
}

// CustomMember implements consistent.Member interface.
type CustomMember string

func (m CustomMember) String() string {
	return string(m)
}

// hasher uses xxhash for Consistent
type hasher struct{}

func (h hasher) Sum64(data []byte) uint64 {
	return xxhash.Sum64(data)
}

// NewConsistentHashSharding returns a consistent hash sharding strategy with default config
func NewConsistentHashSharding() *ConsistentHashSharding {
	return NewConsistentHashShardingWithConfig(DefaultConsistentHashConfig())
}

// NewConsistentHashShardingWithConfig returns a consistent hash sharding strategy with custom config
func NewConsistentHashShardingWithConfig(config ConsistentHashConfig) *ConsistentHashSharding {

	if config.PartitionCount <= 0 {
		config.PartitionCount = 997
	}
	if config.ReplicationFactor <= 0 {
		config.ReplicationFactor = 20
	}
	if config.Load <= 0 {
		config.Load = 1.25
	}

	return &ConsistentHashSharding{
		config: config,
	}
}

// AssignGroups assigns groups to instances using consistent hash
func (s *ConsistentHashSharding) AssignGroups(groups []string, instances []string) map[string][]string {
	if len(instances) == 0 {
		return make(map[string][]string)
	}

	cfg := consistent.Config{
		PartitionCount:    s.config.PartitionCount,
		ReplicationFactor: s.config.ReplicationFactor,
		Load:              s.config.Load,
		Hasher:            hasher{},
	}

	members := make([]consistent.Member, 0, len(instances))
	for _, inst := range instances {
		members = append(members, CustomMember(inst))
	}

	ring := consistent.New(members, cfg)

	// Initialize assignments map with all instances
	assignments := make(map[string][]string)
	for _, instance := range instances {
		assignments[instance] = []string{}
	}

	// Assign groups to instances
	for _, group := range groups {
		member := ring.LocateKey([]byte(group)).String()
		assignments[member] = append(assignments[member], group)
	}

	return assignments
}

// GetConfig returns the current configuration
func (s *ConsistentHashSharding) GetConfig() ConsistentHashConfig {
	return s.config
}
