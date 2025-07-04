package api

import (
	"LagRadar/internal/collector"
	"encoding/json"
	"fmt"
	"net/http"
)

// GroupStatusHandler GET /api/v1/status/{group}
func GroupStatusHandler(c *collector.Collector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID := r.URL.Path[len("/api/v1/status/"):]
		if groupID == "" {
			http.Error(w, "Group ID required", http.StatusBadRequest)
			return
		}
		status, exists := c.GetGroupStatus(groupID)
		if !exists {
			http.Error(w, fmt.Sprintf("Consumer group %s not found", groupID), http.StatusNotFound)
			return
		}
		writeJSON(w, status)
	}
}

// ConfigHandler GET /api/v1/config
func ConfigHandler(config interface{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, config)
	}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(v)
	if err != nil {
		return
	}
}
