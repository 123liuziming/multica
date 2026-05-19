package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// TriggerAoneSync handles POST /api/workspaces/{id}/aone-sync.
// It triggers an immediate Aone→Multica issue sync for the given workspace.
func (h *Handler) TriggerAoneSync(w http.ResponseWriter, r *http.Request) {
	if h.AoneSyncService == nil {
		http.Error(w, `{"error":"aone sync service not initialized"}`, http.StatusServiceUnavailable)
		return
	}

	wsIDStr := chi.URLParam(r, "id")
	wsID, ok := parseUUIDOrBadRequest(w, wsIDStr, "id")
	if !ok {
		return
	}

	ws, err := h.Queries.GetWorkspace(r.Context(), wsID)
	if err != nil {
		http.Error(w, `{"error":"workspace not found"}`, http.StatusNotFound)
		return
	}

	result := h.AoneSyncService.SyncWorkspace(r.Context(), ws)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
