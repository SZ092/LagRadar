package coordinator

import (
	"fmt"
	"testing"
)

func TestConsistentHashSharding(t *testing.T) {
	tests := []struct {
		name      string
		groups    []string
		instances []string
		validate  func(t *testing.T, assignments map[string][]string)
	}{
		{
			name:      "empty instances",
			groups:    []string{"group1", "group2"},
			instances: []string{},
			validate: func(t *testing.T, assignments map[string][]string) {
				if len(assignments) != 0 {
					t.Errorf("expected empty assignments, got %d", len(assignments))
				}
			},
		},
		{
			name:      "single instance",
			groups:    []string{"group1", "group2", "group3"},
			instances: []string{"instance1"},
			validate: func(t *testing.T, assignments map[string][]string) {
				if len(assignments) != 1 {
					t.Errorf("expected 1 assignment, got %d", len(assignments))
				}
				if len(assignments["instance1"]) != 3 {
					t.Errorf("expected 3 groups for instance1, got %d", len(assignments["instance1"]))
				}
			},
		},
		{
			name:      "multiple instances",
			groups:    []string{"group1", "group2", "group3", "group4", "group5", "group6"},
			instances: []string{"instance1", "instance2", "instance3"},
			validate: func(t *testing.T, assignments map[string][]string) {
				if len(assignments) != 3 {
					t.Errorf("expected 3 assignments, got %d", len(assignments))
				}

				totalGroups := 0
				for _, groups := range assignments {
					totalGroups += len(groups)
				}
				if totalGroups != 6 {
					t.Errorf("expected 6 total groups, got %d", totalGroups)
				}
			},
		},
		{
			name:      "no groups",
			groups:    []string{},
			instances: []string{"instance1", "instance2"},
			validate: func(t *testing.T, assignments map[string][]string) {
				if len(assignments) != 2 {
					t.Errorf("expected 2 assignments, got %d", len(assignments))
				}
				for instance, groups := range assignments {
					if len(groups) != 0 {
						t.Errorf("expected 0 groups for %s, got %d", instance, len(groups))
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sharding := NewConsistentHashSharding()
			assignments := sharding.AssignGroups(tt.groups, tt.instances)
			tt.validate(t, assignments)
		})
	}
}

func TestConsistentHashShardingConfig(t *testing.T) {
	tests := []struct {
		name           string
		config         ConsistentHashConfig
		expectedConfig ConsistentHashConfig
	}{
		{
			name: "valid config",
			config: ConsistentHashConfig{
				PartitionCount:    100,
				ReplicationFactor: 10,
				Load:              1.5,
			},
			expectedConfig: ConsistentHashConfig{
				PartitionCount:    100,
				ReplicationFactor: 10,
				Load:              1.5,
			},
		},
		{
			name: "zero values - should use defaults",
			config: ConsistentHashConfig{
				PartitionCount:    0,
				ReplicationFactor: 0,
				Load:              0,
			},
			expectedConfig: ConsistentHashConfig{
				PartitionCount:    997,
				ReplicationFactor: 20,
				Load:              1.25,
			},
		},
		{
			name: "negative values - should use defaults",
			config: ConsistentHashConfig{
				PartitionCount:    -1,
				ReplicationFactor: -1,
				Load:              -1,
			},
			expectedConfig: ConsistentHashConfig{
				PartitionCount:    997,
				ReplicationFactor: 20,
				Load:              1.25,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sharding := NewConsistentHashShardingWithConfig(tt.config)
			actualConfig := sharding.GetConfig()

			if actualConfig.PartitionCount != tt.expectedConfig.PartitionCount {
				t.Errorf("expected PartitionCount %d, got %d",
					tt.expectedConfig.PartitionCount, actualConfig.PartitionCount)
			}
			if actualConfig.ReplicationFactor != tt.expectedConfig.ReplicationFactor {
				t.Errorf("expected ReplicationFactor %d, got %d",
					tt.expectedConfig.ReplicationFactor, actualConfig.ReplicationFactor)
			}
			if actualConfig.Load != tt.expectedConfig.Load {
				t.Errorf("expected Load %f, got %f",
					tt.expectedConfig.Load, actualConfig.Load)
			}
		})
	}
}

func TestConsistentHashDistribution(t *testing.T) {

	sharding := NewConsistentHashSharding()
	groups := []string{"group1", "group2", "group3", "group4", "group5"}
	instances := []string{"instance1", "instance2", "instance3"}

	assignments1 := sharding.AssignGroups(groups, instances)
	assignments2 := sharding.AssignGroups(groups, instances)

	// Verify assignments are consistent
	for instance, groups1 := range assignments1 {
		groups2 := assignments2[instance]
		if len(groups1) != len(groups2) {
			t.Errorf("inconsistent assignment for %s: %d vs %d groups",
				instance, len(groups1), len(groups2))
		}

		// Check each group
		groupMap := make(map[string]bool)
		for _, g := range groups1 {
			groupMap[g] = true
		}
		for _, g := range groups2 {
			if !groupMap[g] {
				t.Errorf("group %s assigned differently in second run", g)
			}
		}
	}
}

func TestDefaultConsistentHashConfig(t *testing.T) {
	config := DefaultConsistentHashConfig()

	if config.PartitionCount != 997 {
		t.Errorf("expected default PartitionCount 997, got %d", config.PartitionCount)
	}
	if config.ReplicationFactor != 20 {
		t.Errorf("expected default ReplicationFactor 20, got %d", config.ReplicationFactor)
	}
	if config.Load != 1.25 {
		t.Errorf("expected default Load 1.25, got %f", config.Load)
	}
}

// Benchmark tests
func BenchmarkConsistentHashAssignment(b *testing.B) {
	sharding := NewConsistentHashSharding()

	groups := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		groups[i] = fmt.Sprintf("group-%d", i)
	}

	instances := make([]string, 10)
	for i := 0; i < 10; i++ {
		instances[i] = fmt.Sprintf("instance-%d", i)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = sharding.AssignGroups(groups, instances)
	}
}
