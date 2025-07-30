package state

import (
	"LagRadar/distributed"
	"LagRadar/internal/collector"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-redis/redis/v8"
	"log"
	"strconv"
	"time"
)

const (
	MaxIterations = 100
)

// StateStore wraps Redis and implements instance, assignment, and partition state persistence.
type StateStore struct {
	Config distributed.StateStoreConfig
	client *redis.Client
}

// NewStateStore creates a StateStore with the provided config and Redis client.
func NewStateStore(cfg distributed.StateStoreConfig, client *redis.Client) *StateStore {
	return &StateStore{
		Config: cfg,
		client: client,
	}
}

// NewStateStoreWithClient creates a StateStore with default config and provided client - test.
func NewStateStoreWithClient(client *redis.Client) *StateStore {
	return &StateStore{
		Config: distributed.StateStoreConfig{
			Prefix:             "lagradar",
			InstanceTTL:        5 * time.Minute,
			InstanceTimeout:    2 * time.Minute,
			PartitionWindowTTL: 24 * time.Hour,
			AssignmentTimeout:  10 * time.Minute,
		},
		client: client,
	}
}

// instanceKey returns the Redis key for storing a specific instance's state.
func (s *StateStore) instanceKey(instanceID string) string {
	return fmt.Sprintf("%s:instance:%s", s.Config.Prefix, instanceID)
}

// windowKey returns the Redis key for a partition window.
func (s *StateStore) windowKey(key string) string {
	return fmt.Sprintf("%s:window:%s", s.Config.Prefix, key)
}

// assignmentsKey returns the Redis key for group assignments hash.
func (s *StateStore) assignmentsKey() string {
	return fmt.Sprintf("%s:assignments", s.Config.Prefix)
}

// assignmentsTimestampKey returns the Redis key for the assignments timestamp.
func (s *StateStore) assignmentsTimestampKey() string {
	return s.assignmentsKey() + ":timestamp"
}

// RegisterInstance creates or updates an instance state as active in Redis.
// The instance record uses a TTL controlled by StateStoreConfig.InstanceTTL.
// Returns error if JSON marshal fails or Redis write fails.
func (s *StateStore) RegisterInstance(ctx context.Context, instanceID string) error {
	info := distributed.InstanceInfo{
		ID:            instanceID,
		LastHeartbeat: time.Now().UTC(),
		Status:        "active",
		Metadata: map[string]string{
			"version":    "1.0.0",
			"start_time": time.Now().UTC().Format(time.RFC3339),
		},
	}

	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal instance info: %w", err)
	}
	key := s.instanceKey(instanceID)
	return s.client.Set(ctx, key, data, s.Config.InstanceTTL).Err()
}

// UpdateHeartbeat renews an instance's TTL in Redis (or registers it if not found).
// Returns error if Redis returns error or TTL cannot be updated.
func (s *StateStore) UpdateHeartbeat(ctx context.Context, instanceID string) error {
	key := s.instanceKey(instanceID)

	exists, err := s.client.Exists(ctx, key).Result()
	if err != nil {
		return err
	}

	if exists == 0 {
		return s.RegisterInstance(ctx, instanceID)
	}
	return s.client.Expire(ctx, key, s.Config.InstanceTTL).Err()
}

// GetActiveInstances returns IDs of all instances considered "active" (heartbeat within timeout, status active).
// Returns error if Redis SCAN or GET fails unexpectedly.
func (s *StateStore) GetActiveInstances(ctx context.Context, timeout time.Duration) ([]string, error) {
	pattern := s.instanceKey("*")
	var cursor uint64
	var instances []string
	now := time.Now().UTC()

	iterations := 0

	for {
		if iterations >= MaxIterations {
			return instances, fmt.Errorf("max iterations reached while scanning instances")
		}
		iterations++

		keys, nextCursor, err := s.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to scan instances: %w", err)
		}
		if len(keys) > 0 {
			pipe := s.client.Pipeline()
			cmds := make([]*redis.StringCmd, len(keys))
			for i, key := range keys {
				cmds[i] = pipe.Get(ctx, key)
			}
			_, err := pipe.Exec(ctx)
			if err != nil && !errors.Is(redis.Nil, err) {
				return nil, fmt.Errorf("pipeline exec error: %w", err)
			}
			for _, cmd := range cmds {
				data, err := cmd.Bytes()
				if err != nil {
					continue
				}
				var info distributed.InstanceInfo
				if err := json.Unmarshal(data, &info); err != nil {
					log.Printf("[StateStore] Warning: Failed to unmarshal instance info: %v", err)
					continue
				}
				if now.Sub(info.LastHeartbeat) < timeout && info.Status == "active" {
					instances = append(instances, info.ID)
				}
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return instances, nil
}

// StoreAssignments stores group assignments for all instances in Redis, with a new timestamp for freshness detection.
// The entire operation is performed in a transaction (TxPipelined).
// Returns error if marshal, Redis pipeline, or transaction fails.
func (s *StateStore) StoreAssignments(ctx context.Context, assignments map[string][]string) error {
	// Marshal all assignments before pipeline to prevent partial writes on marshal error.
	assignmentData := make(map[string]string, len(assignments))
	for instance, groups := range assignments {
		data, err := json.Marshal(groups)
		if err != nil {
			return fmt.Errorf("failed to marshal groups for instance %s: %w", instance, err)
		}
		assignmentData[instance] = string(data)
	}

	_, err := s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, s.assignmentsKey())
		for instance, groupJSON := range assignmentData {
			pipe.HSet(ctx, s.assignmentsKey(), instance, groupJSON)
		}
		pipe.Set(ctx, s.assignmentsTimestampKey(), time.Now().UTC().Unix(), s.Config.AssignmentTimeout*2)
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to store assignments: %w", err)
	}

	log.Printf("[StateStore] Stored assignments for %d instances", len(assignments))
	return nil
}

// GetAssignments fetches group assignments for all instances from Redis.
// If the assignment timestamp key exists and exceeds AssignmentTimeout, returns error and does not return assignments.
// If some assignments' JSON are invalid, those entries will be skipped and a warning logged.
func (s *StateStore) GetAssignments(ctx context.Context) (map[string][]string, error) {
	// Check timestamp if available
	timestampStr, err := s.client.Get(ctx, s.assignmentsTimestampKey()).Result()
	if err == nil {
		timestamp, _ := strconv.ParseInt(timestampStr, 10, 64)
		if timestamp > 0 && time.Since(time.Unix(timestamp, 0)) > s.Config.AssignmentTimeout {
			return nil, fmt.Errorf("assignments expired (last updated %s)", time.Unix(timestamp, 0).UTC())
		}
	}

	data, err := s.client.HGetAll(ctx, s.assignmentsKey()).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get assignments: %w", err)
	}

	assignments := make(map[string][]string)
	for instance, groupsData := range data {
		var groups []string
		if err := json.Unmarshal([]byte(groupsData), &groups); err != nil {
			log.Printf("[StateStore] Warning: Failed to unmarshal groups for instance %s: %v", instance, err)
			continue
		}
		assignments[instance] = groups
	}

	return assignments, nil
}

// StorePartitionWindow stores partition window records as JSON with expiration.
// Does nothing if records is empty. Returns error on marshal or Redis error.
func (s *StateStore) StorePartitionWindow(ctx context.Context, key string, records []collector.OffsetRecord) error {
	if len(records) == 0 {
		return nil
	}

	data, err := json.Marshal(records)
	if err != nil {
		return fmt.Errorf("failed to marshal records: %w", err)
	}

	redisKey := s.windowKey(key)
	return s.client.Set(ctx, redisKey, data, s.Config.PartitionWindowTTL).Err()
}

// GetPartitionWindow retrieves a partition window from Redis and decodes the records.
// Returns (nil, nil) if the key is not found. Returns error on Redis or decode failure.
func (s *StateStore) GetPartitionWindow(ctx context.Context, key string) ([]collector.OffsetRecord, error) {
	redisKey := s.windowKey(key)
	data, err := s.client.Get(ctx, redisKey).Bytes()
	if err != nil {
		if errors.Is(redis.Nil, err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get partition window: %w", err)
	}

	var records []collector.OffsetRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("failed to unmarshal records: %w", err)
	}

	return records, nil
}

// MarkInstanceOffline marks an instance as offline (sets status and LastHeartbeat, TTL=30s).
// If the instance is not found (key missing), does nothing and returns nil (idempotent).
// Returns error if Redis or JSON fails.
func (s *StateStore) MarkInstanceOffline(ctx context.Context, instanceID string) error {
	key := s.instanceKey(instanceID)

	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(redis.Nil, err) {
			return nil // Instance already gone (idempotent)
		}
		return err
	}

	var info distributed.InstanceInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return err
	}

	info.Status = "offline"
	info.LastHeartbeat = time.Now().UTC()

	newData, err := json.Marshal(info)
	if err != nil {
		return err
	}

	return s.client.Set(ctx, key, newData, 30*time.Second).Err()
}
