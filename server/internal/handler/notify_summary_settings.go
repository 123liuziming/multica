package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/notifysummary"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type notifySummarySettingsResponse struct {
	WorkspaceID string                `json:"workspace_id"`
	Settings    notifysummary.Settings `json:"settings"`
}

// GetNotifySummarySettings returns the workspace's per-workspace notify-summary
// configuration. Returns the package defaults when the workspace has never
// configured the feature (handler does not 404 on absence).
func (h *Handler) GetNotifySummarySettings(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "workspace id")
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), idUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace not found")
			return
		}
		slog.Warn("GetWorkspace failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load workspace")
		return
	}
	settings, err := notifysummary.FromWorkspaceSettings(ws.Settings)
	if err != nil {
		// Persistence is fine — just log + serve defaults so the operator can
		// repair via PUT.
		slog.Warn("notify_summary: parse existing settings failed",
			append(logger.RequestAttrs(r), "error", err, "workspace_id", id)...)
	}
	writeJSON(w, http.StatusOK, notifySummarySettingsResponse{
		WorkspaceID: id,
		Settings:    settings,
	})
}

// UpdateNotifySummarySettings persists the workspace's notify-summary config.
// The request body is the bare Settings JSON shape. Validation:
//   - SummaryLength, IdleWaitSecs, MaxWaitSecs are non-negative (zero ⇒ default
//     via Normalize).
//   - Template parses as Go text/template (empty string is OK; renderer falls
//     back to the built-in DefaultTemplate).
//
// The existing JSONB is merged via notifysummary.MergeIntoWorkspaceSettings so
// unrelated keys (e.g. aone_project_id) survive.
func (h *Handler) UpdateNotifySummarySettings(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "workspace id")
	if !ok {
		return
	}

	var req notifysummary.Settings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.SummaryLength < 0 || req.IdleWaitSecs < 0 || req.MaxWaitSecs < 0 {
		writeError(w, http.StatusBadRequest, "summary_length / idle_wait_secs / max_wait_secs must be >= 0")
		return
	}
	if err := notifysummary.ValidateTemplate(req.Template); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ws, err := h.Queries.GetWorkspace(r.Context(), idUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace not found")
			return
		}
		slog.Warn("GetWorkspace failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load workspace")
		return
	}
	merged, err := notifysummary.MergeIntoWorkspaceSettings(ws.Settings, req)
	if err != nil {
		slog.Warn("notify_summary: merge failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to merge settings")
		return
	}
	updated, err := h.Queries.UpdateWorkspace(r.Context(), db.UpdateWorkspaceParams{
		ID:       idUUID,
		Settings: merged,
	})
	if err != nil {
		slog.Warn("UpdateWorkspace failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to persist settings: "+err.Error())
		return
	}
	finalSettings, _ := notifysummary.FromWorkspaceSettings(updated.Settings)
	writeJSON(w, http.StatusOK, notifySummarySettingsResponse{
		WorkspaceID: id,
		Settings:    finalSettings,
	})
}
