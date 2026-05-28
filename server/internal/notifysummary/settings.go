// Package notifysummary owns the per-(staff, issue) inbox summarization
// pipeline that multica feeds into cc-connect's /notify-session endpoint.
//
// Workspace settings, template rendering, bucket throttling, and the HTTP
// call to cc-connect all live here. cc-connect itself is a dumb pipe that
// just injects the rendered prompt into the user's active 1:1 session.
package notifysummary

import (
	"encoding/json"
	"fmt"
)

// Settings is the per-workspace notify-summary configuration, stored under
// the "notify_summary" key inside workspace.settings JSONB. All fields are
// JSON-tagged so the same struct round-trips to the REST API and back.
type Settings struct {
	Enabled       bool   `json:"enabled"`
	Template      string `json:"template"`
	IdleWaitSecs  int    `json:"idle_wait_secs"`
	MaxWaitSecs   int    `json:"max_wait_secs"`
	SummaryLength int    `json:"summary_length"`
}

// settingsKey is the JSON object key inside workspace.settings.
const settingsKey = "notify_summary"

// Default returns the zero-config Settings used when a workspace has never
// configured the feature. Enabled is false so the existing direct push path
// remains the default.
func Default() Settings {
	return Settings{
		Enabled:       false,
		Template:      "", // renderer falls back to DefaultTemplate
		IdleWaitSecs:  10,
		MaxWaitSecs:   30,
		SummaryLength: 300,
	}
}

// Normalize fills zero values from Default() and clamps MaxWaitSecs to at
// least IdleWaitSecs. Callers should always run Normalize before persisting
// or using Settings.
func Normalize(s Settings) Settings {
	d := Default()
	if s.IdleWaitSecs <= 0 {
		s.IdleWaitSecs = d.IdleWaitSecs
	}
	if s.MaxWaitSecs <= 0 {
		s.MaxWaitSecs = d.MaxWaitSecs
	}
	if s.SummaryLength <= 0 {
		s.SummaryLength = d.SummaryLength
	}
	if s.MaxWaitSecs < s.IdleWaitSecs {
		s.MaxWaitSecs = s.IdleWaitSecs
	}
	return s
}

// FromWorkspaceSettings parses the workspace.settings JSONB blob and returns
// the embedded notify_summary Settings. Returns Default() (plus a non-nil
// error explaining why) when the blob is malformed or the key is absent.
// The dispatcher path treats any non-nil error as "use defaults"; the REST
// GET handler surfaces it to the operator.
func FromWorkspaceSettings(raw []byte) (Settings, error) {
	if len(raw) == 0 {
		return Default(), nil
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return Default(), fmt.Errorf("notifysummary: parse workspace settings: %w", err)
	}
	sub, ok := top[settingsKey]
	if !ok || len(sub) == 0 || string(sub) == "null" {
		return Default(), nil
	}
	var s Settings
	if err := json.Unmarshal(sub, &s); err != nil {
		return Default(), fmt.Errorf("notifysummary: parse %s sub-object: %w", settingsKey, err)
	}
	return Normalize(s), nil
}

// MergeIntoWorkspaceSettings overwrites the notify_summary sub-object inside
// the workspace.settings JSONB without touching other keys (e.g. the existing
// aone_project_id). Mirrors the application-level merge pattern used by the
// workspace.UpdateWorkspace handler so unrelated settings survive a PUT.
func MergeIntoWorkspaceSettings(raw []byte, s Settings) ([]byte, error) {
	top := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &top); err != nil {
			return nil, fmt.Errorf("notifysummary: parse existing settings: %w", err)
		}
	}
	top[settingsKey] = Normalize(s)
	out, err := json.Marshal(top)
	if err != nil {
		return nil, fmt.Errorf("notifysummary: marshal merged settings: %w", err)
	}
	return out, nil
}
