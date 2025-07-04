package api

import (
	"LagRadar/internal/cluster"
	"encoding/json"
	"net/http"
	"runtime"
	"time"
)

// HealthHandler - K8s Liveness Probe
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"service":   "lagradar",
		"runtime": map[string]interface{}{
			"goroutines":      runtime.NumGoroutine(),
			"memory_alloc_mb": float64(getMemStats().Alloc) / 1024 / 1024,
		},
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(health)
}

// ClusterReadyHandler - K8s Readiness Probe
func ClusterReadyHandler(manager *cluster.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		clusters := manager.GetAllClusters()

		if len(clusters) == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":    "not_ready",
				"reason":    "no_clusters_configured",
				"timestamp": time.Now().UTC(),
			})
			return
		}

		readyClusters := 0
		totalClusters := len(clusters)
		clusterDetails := make(map[string]interface{})

		for name, cc := range clusters {
			clusterInfo := map[string]interface{}{
				"name":  name,
				"ready": cc.Collector.IsReady(),
			}

			if cc.Collector.IsReady() {
				readyClusters++
				groups := cc.Collector.GetAllGroupStatuses()
				clusterInfo["consumer_groups"] = len(groups)
			}

			if err := cc.GetLastError(); err != nil {
				clusterInfo["last_error"] = err.Error()
			}

			clusterDetails[name] = clusterInfo
		}

		if readyClusters > 0 {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":         "ready",
				"ready_clusters": readyClusters,
				"total_clusters": totalClusters,
				"timestamp":      time.Now().UTC(),
				"clusters":       clusterDetails,
			})
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":         "not_ready",
				"reason":         "no_ready_clusters",
				"ready_clusters": 0,
				"total_clusters": totalClusters,
				"timestamp":      time.Now().UTC(),
				"clusters":       clusterDetails,
			})
		}
	}
}

func getMemStats() runtime.MemStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m
}
