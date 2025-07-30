package cluster

import (
	"LagRadar/internal/collector"
	"LagRadar/internal/publisher"
	"LagRadar/internal/rca"
	"context"
	"fmt"
	"log"
	"sync"
)

// ConfigCluster represents configuration for a single Kafka cluster
type ConfigCluster struct {
	Name      string            `yaml:"name"`
	Enabled   bool              `yaml:"enabled"`
	Brokers   []string          `yaml:"brokers"`
	Collector *collector.Config `yaml:"collector,omitempty"`
}

// Manager manages multiple Kafka cluster collectors
type Manager struct {
	clusters     map[string]*CollectorCluster
	clustersMu   sync.RWMutex
	globalConfig collector.Config
	rcaConfig    *rca.Config
	ctx          context.Context
	cancel       context.CancelFunc
}

// CollectorCluster wraps a collector with cluster metadata
type CollectorCluster struct {
	ClusterName  string
	Collector    *collector.Collector
	Config       ConfigCluster
	LastError    error
	LastErrorMu  sync.RWMutex
	RCAPublisher *publisher.EventPublisher   // RCA publisher for this cluster
	Evaluator    *publisher.EvaluatorWithRCA // RCA-aware evaluator
	cancelFunc   context.CancelFunc          // Cancel function for this cluster
}

// NewManager creates a new cluster manager
func NewManager(globalConfig collector.Config) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		clusters:     make(map[string]*CollectorCluster),
		globalConfig: globalConfig,
		ctx:          ctx,
		cancel:       cancel,
	}
}

// NewManagerWithRCA creates a new cluster manager with RCA support
func NewManagerWithRCA(globalConfig collector.Config, rcaConfig rca.Config) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		clusters:     make(map[string]*CollectorCluster),
		globalConfig: globalConfig,
		ctx:          ctx,
		cancel:       cancel,
		rcaConfig:    &rcaConfig,
	}
}

// AddCluster adds a new cluster to be monitored
func (m *Manager) AddCluster(config ConfigCluster) error {
	if !config.Enabled {
		log.Printf("Cluster %s is disabled, skipping", config.Name)
		return nil
	}

	m.clustersMu.Lock()
	defer m.clustersMu.Unlock()

	// Check if cluster already exists
	if _, exists := m.clusters[config.Name]; exists {
		return fmt.Errorf("cluster %s already exists", config.Name)
	}

	// Use global config or cluster-specific config if presented
	collectorConfig := m.globalConfig
	if config.Collector != nil {
		collectorConfig = *config.Collector
	}

	brokers := joinBrokers(config.Brokers)

	// Create collector with optional RCA support
	coll, rcaPublisher, err := createCollectorWithRCA(brokers, collectorConfig, m.rcaConfig, config.Name)

	if err != nil {
		return fmt.Errorf("failed to create collector for cluster %s: %w", config.Name, err)
	}

	// Create cluster context
	clusterCtx, clusterCancel := context.WithCancel(m.ctx)

	// Wrap in ClusterCollector
	cc := &CollectorCluster{
		ClusterName:  config.Name,
		Collector:    coll,
		Config:       config,
		RCAPublisher: rcaPublisher,
		cancelFunc:   clusterCancel,
	}

	m.clusters[config.Name] = cc

	// Start periodic collection for this cluster
	go m.startClusterCollection(clusterCtx, cc)

	log.Printf("Added cluster %s with %d brokers (RCA: %v)",
		config.Name, len(config.Brokers), rcaPublisher != nil)

	return nil
}

// RemoveCluster removes a cluster from monitoring
func (m *Manager) RemoveCluster(clusterName string) error {
	m.clustersMu.Lock()
	defer m.clustersMu.Unlock()

	cc, exists := m.clusters[clusterName]
	if !exists {
		return fmt.Errorf("cluster %s not found", clusterName)
	}

	// Cancel the cluster's context
	if cc.cancelFunc != nil {
		cc.cancelFunc()
	}

	cc.Collector.Close()

	if cc.RCAPublisher != nil {
		cc.RCAPublisher.Close()
	}

	delete(m.clusters, clusterName)

	log.Printf("Removed cluster %s", clusterName)
	return nil
}

// GetCluster returns a specific cluster collector
func (m *Manager) GetCluster(clusterName string) (*CollectorCluster, bool) {
	m.clustersMu.RLock()
	defer m.clustersMu.RUnlock()

	cc, exists := m.clusters[clusterName]
	return cc, exists
}

// GetAllClusters returns all cluster collectors
func (m *Manager) GetAllClusters() map[string]*CollectorCluster {
	m.clustersMu.RLock()
	defer m.clustersMu.RUnlock()

	result := make(map[string]*CollectorCluster)
	for k, v := range m.clusters {
		result[k] = v
	}
	return result
}

// GetAllGroupStatuses returns group statuses across all clusters
func (m *Manager) GetAllGroupStatuses() map[string]GroupStatusCluster {
	result := make(map[string]GroupStatusCluster)

	for clusterName, cc := range m.GetAllClusters() {
		statuses := cc.Collector.GetAllGroupStatuses()
		for groupID, status := range statuses {
			key := fmt.Sprintf("%s/%s", clusterName, groupID)
			result[key] = GroupStatusCluster{
				ClusterName: clusterName,
				GroupStatus: status,
			}
		}
	}

	return result
}

// GetClusterGroupStatuses returns group statuses for a specific cluster
func (m *Manager) GetClusterGroupStatuses(clusterName string) (map[string]GroupStatusCluster, error) {
	cc, exists := m.GetCluster(clusterName)
	if !exists {
		return nil, fmt.Errorf("cluster %s not found", clusterName)
	}

	statuses := cc.Collector.GetAllGroupStatuses()

	result := make(map[string]GroupStatusCluster, len(statuses))
	for group, status := range statuses {
		result[group] = GroupStatusCluster{
			ClusterName: clusterName,
			GroupStatus: status,
		}
	}

	return result, nil
}

// GetGroupStatus returns status for a specific group in a specific cluster
func (m *Manager) GetGroupStatus(clusterName, groupID string) (GroupStatusCluster, bool, error) {
	cc, exists := m.GetCluster(clusterName)
	if !exists {
		return GroupStatusCluster{}, false, fmt.Errorf("cluster %s not found", clusterName)
	}

	status, found := cc.Collector.GetGroupStatus(groupID)

	if !found {
		return GroupStatusCluster{}, false, nil
	}

	return GroupStatusCluster{
		ClusterName: clusterName,
		GroupStatus: status,
	}, true, nil
}

// startClusterCollection starts periodic collection for a cluster
func (m *Manager) startClusterCollection(ctx context.Context, cc *CollectorCluster) {
	cc.Collector.StartPeriodicCollection(ctx, cc.ClusterName)
}

// Stop stops all cluster collectors
func (m *Manager) Stop() {
	m.cancel()

	m.clustersMu.Lock()
	defer m.clustersMu.Unlock()

	for _, cc := range m.clusters {
		if cc.cancelFunc != nil {
			cc.cancelFunc()
		}
		cc.Collector.Close()
		if cc.RCAPublisher != nil {
			cc.RCAPublisher.Close()
		}
	}

	log.Println("Cluster manager stopped")
}

// GroupStatusCluster  wraps a GroupStatus with cluster information
type GroupStatusCluster struct {
	ClusterName string
	collector.GroupStatus
}

func (cc *CollectorCluster) GetLastError() error {
	cc.LastErrorMu.RLock()
	defer cc.LastErrorMu.RUnlock()
	return cc.LastError
}

func joinBrokers(brokers []string) string {
	result := ""
	for i, broker := range brokers {
		if i > 0 {
			result += ","
		}
		result += broker
	}
	return result
}
