package state

import (
	"LagRadar/distributed"
	"LagRadar/internal/collector"
	"context"
	"encoding/json"
	"fmt"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/go-redis/redismock/v8"
	"testing"
	"time"
)

func setupMiniredis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to run miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	return client, mr
}

func TestStateStore_RegisterInstance(t *testing.T) {
	client, mr := setupMiniredis(t)
	defer mr.Close()

	store := NewStateStoreWithClient(client)
	ctx := context.Background()
	instanceID := "test-instance"

	err := store.RegisterInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the instance was registered
	key := fmt.Sprintf("lagradar:instance:%s", instanceID)
	data, err := client.Get(ctx, key).Result()
	if err != nil {
		t.Fatalf("failed to get instance data: %v", err)
	}

	var info distributed.InstanceInfo
	if err := json.Unmarshal([]byte(data), &info); err != nil {
		t.Fatalf("failed to unmarshal instance info: %v", err)
	}

	// Verify the content
	if info.ID != instanceID {
		t.Errorf("expected ID %s, got %s", instanceID, info.ID)
	}
	if info.Status != "active" {
		t.Errorf("expected status 'active', got %s", info.Status)
	}
	if info.Metadata["version"] != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", info.Metadata["version"])
	}

	// Verify TTL
	ttl, err := client.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("failed to get TTL: %v", err)
	}
	if ttl < 2*time.Minute || ttl > store.Config.InstanceTTL {
		t.Errorf("unexpected TTL: %v", ttl)
	}
}

func TestStateStore_UpdateHeartbeat(t *testing.T) {
	client, mr := setupMiniredis(t)
	defer mr.Close()

	store := NewStateStoreWithClient(client)
	ctx := context.Background()
	instanceID := "test-instance"

	err := store.UpdateHeartbeat(ctx, instanceID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	key := fmt.Sprintf("lagradar:instance:%s", instanceID)
	exists, _ := client.Exists(ctx, key).Result()
	if exists != 1 {
		t.Error("expected instance to be created")
	}

	mr.FastForward(60 * time.Second)

	ttlBefore, err := client.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("failed to get TTL before: %v", err)
	}

	// Update heartbeat
	err = store.UpdateHeartbeat(ctx, instanceID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ttlAfter, err := client.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("failed to get TTL after: %v", err)
	}

	// TTL should be renewed (close to InstanceTTL)
	if ttlAfter <= ttlBefore {
		t.Errorf("expected TTL to be renewed: before=%v, after=%v", ttlBefore, ttlAfter)
	}

	// Verify TTL is close to InstanceTTL
	expectedTTL := store.Config.InstanceTTL
	tolerance := 5 * time.Second
	if ttlAfter < expectedTTL-tolerance || ttlAfter > expectedTTL+tolerance {
		t.Errorf("TTL not in expected range: got %v, expected ~%v", ttlAfter, expectedTTL)
	}
}

func TestStateStore_UpdateHeartbeat_Error(t *testing.T) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	store := NewStateStoreWithClient(db)
	ctx := context.Background()
	instanceID := "test-instance"
	key := fmt.Sprintf("lagradar:instance:%s", instanceID)

	// Test EXISTS error
	mock.ExpectExists(key).SetErr(fmt.Errorf("redis error"))

	err := store.UpdateHeartbeat(ctx, instanceID)
	if err == nil {
		t.Error("expected error, got nil")
	}

	// Test EXPIRE error
	mock.ExpectExists(key).SetVal(1)
	mock.ExpectExpire(key, store.Config.InstanceTTL).SetErr(fmt.Errorf("expire error"))

	err = store.UpdateHeartbeat(ctx, instanceID)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestStateStore_GetActiveInstances(t *testing.T) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	store := NewStateStoreWithClient(db)
	ctx := context.Background()

	// Mock SCAN results
	keys := []string{
		"lagradar:instance:instance1",
		"lagradar:instance:instance2",
		"lagradar:instance:instance3",
	}

	mock.ExpectScan(0, "lagradar:instance:*", 100).SetVal(keys, 0)

	// Mock pipeline for batch GET
	now := time.Now()
	instances := []distributed.InstanceInfo{
		{ID: "instance1", LastHeartbeat: now, Status: "active"},
		{ID: "instance2", LastHeartbeat: now.Add(-3 * time.Minute), Status: "active"}, // Expired
		{ID: "instance3", LastHeartbeat: now.Add(-30 * time.Second), Status: "active"},
	}

	// Pipeline expectations
	for i, info := range instances {
		data, _ := json.Marshal(info)
		mock.ExpectGet(keys[i]).SetVal(string(data))
	}

	activeInstances, err := store.GetActiveInstances(ctx, store.Config.InstanceTimeout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only return active instances (not expired)
	expectedCount := 2 // instance1 and instance3
	if len(activeInstances) != expectedCount {
		t.Errorf("expected %d active instances, got %d", expectedCount, len(activeInstances))
	}

	// Verify correct instances
	expectedInstances := map[string]bool{"instance1": true, "instance3": true}
	for _, id := range activeInstances {
		if !expectedInstances[id] {
			t.Errorf("unexpected instance: %s", id)
		}
		delete(expectedInstances, id)
	}

	if len(expectedInstances) > 0 {
		t.Errorf("missing expected instances: %v", expectedInstances)
	}
}

func TestStateStore_GetActiveInstances_WithOfflineInstance(t *testing.T) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	store := NewStateStoreWithClient(db)
	ctx := context.Background()

	keys := []string{
		"lagradar:instance:instance1",
		"lagradar:instance:instance2",
	}

	mock.ExpectScan(0, "lagradar:instance:*", 100).SetVal(keys, 0)

	now := time.Now()
	instances := []distributed.InstanceInfo{
		{ID: "instance1", LastHeartbeat: now, Status: "active"},
		{ID: "instance2", LastHeartbeat: now, Status: "offline"}, // Offline status
	}

	for i, info := range instances {
		data, _ := json.Marshal(info)
		mock.ExpectGet(keys[i]).SetVal(string(data))
	}

	activeInstances, err := store.GetActiveInstances(ctx, store.Config.InstanceTimeout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only return active status instances
	if len(activeInstances) != 1 {
		t.Errorf("expected 1 active instance, got %d", len(activeInstances))
	}

	if len(activeInstances) > 0 && activeInstances[0] != "instance1" {
		t.Errorf("expected instance1, got %s", activeInstances[0])
	}
}

func TestStateStore_StoreAssignments(t *testing.T) {
	client, mr := setupMiniredis(t)
	defer mr.Close()

	store := NewStateStoreWithClient(client)
	ctx := context.Background()

	assignments := map[string][]string{
		"instance1": {"group1", "group2"},
		"instance2": {"group3", "group4", "group5"},
	}

	// Store assignments
	err := store.StoreAssignments(ctx, assignments)
	if err != nil {
		t.Fatalf("failed to store assignments: %v", err)
	}

	// Retrieve assignments
	retrieved, err := store.GetAssignments(ctx)
	if err != nil {
		t.Fatalf("failed to get assignments: %v", err)
	}

	// Verify
	if len(retrieved) != len(assignments) {
		t.Errorf("expected %d instances, got %d", len(assignments), len(retrieved))
	}

	for instance, groups := range assignments {
		retrievedGroups, ok := retrieved[instance]
		if !ok {
			t.Errorf("missing instance %s", instance)
			continue
		}

		if len(retrievedGroups) != len(groups) {
			t.Errorf("instance %s: expected %d groups, got %d",
				instance, len(groups), len(retrievedGroups))
		}

		for i, group := range groups {
			if retrievedGroups[i] != group {
				t.Errorf("instance %s: expected group %s at index %d, got %s",
					instance, group, i, retrievedGroups[i])
			}
		}
	}

	// Verify timestamp was set
	timestamp, err := client.Get(ctx, "lagradar:assignments:timestamp").Result()
	if err != nil {
		t.Errorf("failed to get timestamp: %v", err)
	}
	if timestamp == "" {
		t.Error("expected timestamp to be set")
	}
}

func TestStateStore_StoreAssignments_Error(t *testing.T) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	store := NewStateStoreWithClient(db)
	ctx := context.Background()

	assignments := map[string][]string{
		"instance1": {"group1"},
	}

	// Simulate transaction error
	mock.ExpectTxPipeline()
	mock.ExpectDel("lagradar:assignments").SetErr(fmt.Errorf("delete error"))
	mock.ExpectTxPipelineExec()

	err := store.StoreAssignments(ctx, assignments)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestStateStore_GetAssignments(t *testing.T) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	store := NewStateStoreWithClient(db)
	ctx := context.Background()

	// Mock HGetAll response
	data := map[string]string{
		"instance1": `["group1","group2"]`,
		"instance2": `["group3","group4","group5"]`,
	}

	mock.ExpectHGetAll(store.assignmentsKey()).SetVal(data)

	assignments, err := store.GetAssignments(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify assignments
	if len(assignments) != 2 {
		t.Errorf("expected 2 instances, got %d", len(assignments))
	}

	if len(assignments["instance1"]) != 2 {
		t.Errorf("expected 2 groups for instance1, got %d", len(assignments["instance1"]))
	}

	if len(assignments["instance2"]) != 3 {
		t.Errorf("expected 3 groups for instance2, got %d", len(assignments["instance2"]))
	}

	// Verify specific groups
	expectedGroups1 := []string{"group1", "group2"}
	for i, group := range assignments["instance1"] {
		if group != expectedGroups1[i] {
			t.Errorf("expected group %s, got %s", expectedGroups1[i], group)
		}
	}
}

func TestStateStore_GetAssignments_InvalidJSON(t *testing.T) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	store := NewStateStoreWithClient(db)
	ctx := context.Background()

	// Mock HGetAll with invalid JSON
	data := map[string]string{
		"instance1": `["group1","group2"]`, // Valid
		"instance2": `invalid json`,        // Invalid
		"instance3": `["group3"]`,          // Valid
	}

	mock.ExpectHGetAll(store.assignmentsKey()).SetVal(data)

	assignments, err := store.GetAssignments(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only have valid instances
	if len(assignments) != 2 {
		t.Errorf("expected 2 valid assignments, got %d", len(assignments))
	}

	if _, exists := assignments["instance1"]; !exists {
		t.Error("expected instance1 to exist in assignments")
	}

	if _, exists := assignments["instance3"]; !exists {
		t.Error("expected instance3 to exist in assignments")
	}

	if _, exists := assignments["instance2"]; exists {
		t.Error("expected instance2 to be skipped due to invalid JSON")
	}
}

func TestStateStore_StorePartitionWindow(t *testing.T) {
	client, mr := setupMiniredis(t)
	defer mr.Close()

	store := NewStateStoreWithClient(client)
	ctx := context.Background()

	now := time.Now()
	records := []collector.OffsetRecord{
		{CheckTimestamp: now, Offset: 100, HighWatermark: 150, Lag: 50},
		{CheckTimestamp: now.Add(-1 * time.Minute), Offset: 90, HighWatermark: 140, Lag: 50},
	}

	key := "test-group:test-topic:0"
	redisKey := fmt.Sprintf("lagradar:window:%s", key)

	// Store records
	err := store.StorePartitionWindow(ctx, key, records)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify data was stored
	storedData, err := client.Get(ctx, redisKey).Result()
	if err != nil {
		t.Fatalf("failed to get stored data: %v", err)
	}

	var storedRecords []collector.OffsetRecord
	if err := json.Unmarshal([]byte(storedData), &storedRecords); err != nil {
		t.Fatalf("failed to unmarshal stored records: %v", err)
	}

	// Verify content
	if len(storedRecords) != len(records) {
		t.Errorf("expected %d records, got %d", len(records), len(storedRecords))
	}

	for i, record := range storedRecords {
		if record.Offset != records[i].Offset {
			t.Errorf("record %d: expected offset %d, got %d", i, records[i].Offset, record.Offset)
		}
		if record.Lag != records[i].Lag {
			t.Errorf("record %d: expected lag %d, got %d", i, records[i].Lag, record.Lag)
		}
	}

	// Verify TTL
	ttl, err := client.TTL(ctx, redisKey).Result()
	if err != nil {
		t.Fatalf("failed to get TTL: %v", err)
	}

	expectedTTL := store.Config.PartitionWindowTTL
	tolerance := 5 * time.Second
	if ttl < expectedTTL-tolerance || ttl > expectedTTL {
		t.Errorf("unexpected TTL: got %v, expected ~%v", ttl, expectedTTL)
	}

	// Test with empty records - should not store anything
	err = store.StorePartitionWindow(ctx, "empty-key", []collector.OffsetRecord{})
	if err != nil {
		t.Fatalf("unexpected error with empty records: %v", err)
	}

	// Verify empty key was not created
	exists, _ := client.Exists(ctx, "lagradar:window:empty-key").Result()
	if exists != 0 {
		t.Error("expected no key for empty records")
	}
}

func TestStateStore_GetPartitionWindow(t *testing.T) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	store := NewStateStoreWithClient(db)
	ctx := context.Background()

	key := "test-group:test-topic:0"
	redisKey := fmt.Sprintf("lagradar:window:%s", key)

	// Test successful get
	now := time.Now()
	records := []collector.OffsetRecord{
		{CheckTimestamp: now, Offset: 100, HighWatermark: 150, Lag: 50},
		{CheckTimestamp: now.Add(-1 * time.Minute), Offset: 90, HighWatermark: 140, Lag: 50},
	}
	data, _ := json.Marshal(records)

	mock.ExpectGet(redisKey).SetVal(string(data))

	result, err := store.GetPartitionWindow(ctx, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != len(records) {
		t.Errorf("expected %d records, got %d", len(records), len(result))
	}

	// Test key not found
	mock.ExpectGet(redisKey).RedisNil()

	result, err = store.GetPartitionWindow(ctx, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != nil {
		t.Error("expected nil result for non-existent key")
	}

	// Test unmarshal error
	mock.ExpectGet(redisKey).SetVal("invalid json")

	_, err = store.GetPartitionWindow(ctx, key)
	if err == nil {
		t.Error("expected unmarshal error, got nil")
	}
}

func TestStateStore_MarkInstanceOffline(t *testing.T) {
	client, mr := setupMiniredis(t)
	defer mr.Close()

	store := NewStateStoreWithClient(client)
	ctx := context.Background()
	instanceID := "test-instance"
	key := fmt.Sprintf("lagradar:instance:%s", instanceID)

	// Test marking existing instance offline
	existingInfo := distributed.InstanceInfo{
		ID:            instanceID,
		LastHeartbeat: time.Now().Add(-1 * time.Minute),
		Status:        "active",
		Metadata: map[string]string{
			"version": "1.0.0",
		},
	}
	existingData, _ := json.Marshal(existingInfo)

	// Set up existing instance
	err := client.Set(ctx, key, existingData, 0).Err()
	if err != nil {
		t.Fatalf("failed to set up test data: %v", err)
	}

	// Mark offline
	err = store.MarkInstanceOffline(ctx, instanceID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify instance is marked offline
	updatedData, err := client.Get(ctx, key).Result()
	if err != nil {
		t.Fatalf("failed to get updated data: %v", err)
	}

	var updatedInfo distributed.InstanceInfo
	if err := json.Unmarshal([]byte(updatedData), &updatedInfo); err != nil {
		t.Fatalf("failed to unmarshal updated info: %v", err)
	}

	if updatedInfo.Status != "offline" {
		t.Errorf("expected status 'offline', got %s", updatedInfo.Status)
	}

	if updatedInfo.ID != instanceID {
		t.Errorf("expected ID %s, got %s", instanceID, updatedInfo.ID)
	}

	// Verify TTL is set to 30 seconds
	ttl, err := client.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("failed to get TTL: %v", err)
	}

	expectedTTL := 30 * time.Second
	tolerance := 2 * time.Second
	if ttl < expectedTTL-tolerance || ttl > expectedTTL+tolerance {
		t.Errorf("unexpected TTL: got %v, expected ~%v", ttl, expectedTTL)
	}

	// Test marking non-existent instance - should return nil
	nonExistentID := "non-existent"
	err = store.MarkInstanceOffline(ctx, nonExistentID)
	if err != nil {
		t.Fatalf("expected nil error for non-existent instance, got: %v", err)
	}
}
