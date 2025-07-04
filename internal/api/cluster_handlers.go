package api

import (
	"LagRadar/internal/cluster"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ClusterHandlers provides HTTP handlers for multi-cluster operations
type ClusterHandlers struct {
	manager *cluster.Manager
}

// NewClusterHandlers creates new cluster handlers
func NewClusterHandlers(manager *cluster.Manager) *ClusterHandlers {
	return &ClusterHandlers{
		manager: manager,
	}
}

// ListClusters GET /api/v1/clusters
func (h *ClusterHandlers) ListClusters(w http.ResponseWriter, r *http.Request) {
	clusters := h.manager.GetAllClusters()

	type ClusterInfo struct {
		Name      string `json:"name"`
		Enabled   bool   `json:"enabled"`
		Brokers   int    `json:"brokers"`
		Groups    int    `json:"groups"`
		LastError string `json:"last_error,omitempty"`
	}

	result := make([]ClusterInfo, 0, len(clusters))
	for name, cc := range clusters {
		statuses := cc.Collector.GetAllGroupStatuses()
		info := ClusterInfo{
			Name:    name,
			Enabled: true,
			Brokers: len(cc.Config.Brokers),
			Groups:  len(statuses),
		}

		if err := cc.GetLastError(); err != nil {
			info.LastError = err.Error()
		}

		result = append(result, info)
	}

	writeJSON(w, result)
}

// GetClusterStatus GET /api/v1/clusters/{cluster}/status
func (h *ClusterHandlers) GetClusterStatus(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/clusters/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "status" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	clusterName := parts[0]
	statuses, err := h.manager.GetClusterGroupStatuses(clusterName)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": err.Error(),
		})

		return
	}

	writeJSON(w, statuses)
}

// GetClusterGroups GET /api/v1/clusters/{cluster}/groups
func (h *ClusterHandlers) GetClusterGroups(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/clusters/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "groups" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	clusterName := parts[0]
	statuses, err := h.manager.GetClusterGroupStatuses(clusterName)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": err.Error(),
		})

		return
	}

	groupIDs := make([]string, 0, len(statuses))
	for groupID := range statuses {
		groupIDs = append(groupIDs, groupID)
	}

	writeJSON(w, groupIDs)
}

// GetClusterGroupStatus GET /api/v1/clusters/{cluster}/groups/{group}
func (h *ClusterHandlers) GetClusterGroupStatus(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/clusters/")
	parts := strings.Split(path, "/")

	if len(parts) < 3 || parts[1] != "groups" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	clusterName := parts[0]
	groupID := strings.Join(parts[2:], "/")

	status, found, err := h.manager.GetGroupStatus(clusterName, groupID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Cluster error: %v", err),
		})

		return
	}

	if !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Consumer group '%s' not found in cluster '%s'", groupID, clusterName),
		})
		return
	}

	writeJSON(w, status)
}

// GetAllGroupsAcrossClusters GET /api/v1/status - aggregated
func (h *ClusterHandlers) GetAllGroupsAcrossClusters(w http.ResponseWriter, r *http.Request) {
	allStatuses := h.manager.GetAllGroupStatuses()

	// Wrapper to include cluster information
	type GroupWithCluster struct {
		Cluster string      `json:"cluster"`
		Group   string      `json:"group"`
		Status  interface{} `json:"status"`
	}

	result := make([]GroupWithCluster, 0, len(allStatuses))
	for key, status := range allStatuses {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) == 2 {
			result = append(result, GroupWithCluster{
				Cluster: parts[0],
				Group:   parts[1],
				Status:  status.GroupStatus,
			})
		}
	}

	writeJSON(w, result)
}

// GetAllGroupsList GET /api/v1/groups - list all groups with cluster info
func (h *ClusterHandlers) GetAllGroupsList(w http.ResponseWriter, r *http.Request) {
	allStatuses := h.manager.GetAllGroupStatuses()

	type GroupIdentifier struct {
		Cluster string `json:"cluster"`
		Group   string `json:"group"`
	}

	result := make([]GroupIdentifier, 0, len(allStatuses))
	for key := range allStatuses {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) == 2 {
			result = append(result, GroupIdentifier{
				Cluster: parts[0],
				Group:   parts[1],
			})
		}
	}

	writeJSON(w, result)
}
