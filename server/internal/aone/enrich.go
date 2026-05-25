// Package aone fetches metadata about Aone (Alibaba code-review) merge
// requests via the platform's REST API. The handler layer calls Enrich
// inline on first link; the refresh loop later re-runs it on stale rows.
//
// MULTICA_AONE_PRIVATE_TOKEN is the only configuration — without it,
// Enrich returns ErrAoneNotConfigured and callers fall back to the
// URL-derived title. The API host is hard-coded because code.alibaba-inc.com
// is the only deployment that exists.
package aone

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// aoneAPIBase is a var (not const) so tests can point Enrich at an
// httptest.Server. Production code never writes to it.
var aoneAPIBase = "https://code.alibaba-inc.com"

// Enrichment is the subset of MR metadata we surface to the rest of the
// system. State is mapped to the same vocabulary github_pull_request
// uses (open/draft/closed/merged/unknown).
type Enrichment struct {
	Title string
	State string
}

var (
	// ErrAoneNotConfigured is returned when no PRIVATE-TOKEN is set. The
	// caller is expected to fall back to a URL-derived title rather than
	// surfacing this to the user — operators may legitimately run multica
	// without Aone integration configured.
	ErrAoneNotConfigured = errors.New("aone enrichment not configured")
	// ErrAoneRequestFailed wraps transport-level and non-2xx failures.
	ErrAoneRequestFailed = errors.New("aone request failed")
	// ErrAoneParseFailed wraps response parsing failures (empty title,
	// malformed JSON, etc.). Distinct from request failure so the refresh
	// loop can decide which errors are worth retrying urgently.
	ErrAoneParseFailed = errors.New("aone response parse failed")
)

// httpClient is overridable for tests. 10s timeout matches the previous
// shell-out budget and is long enough for the API's typical p99.
var httpClient = &http.Client{Timeout: 10 * time.Second}

// Enrich looks up the MR identified by owner/repo + global id (the number
// in the codereview URL). Returns ErrAoneNotConfigured when the deployment
// hasn't been wired up — callers should swallow it silently.
func Enrich(ctx context.Context, owner, repo string, number int32) (Enrichment, error) {
	token := privateToken()
	if token == "" {
		return Enrichment{}, ErrAoneNotConfigured
	}

	projectID := url.PathEscape(owner + "/" + repo)
	// Note: the path segment is "merge_request" (singular) and the number
	// is the GLOBAL id (the one in the URL), not the per-project iid.
	// We probed both `merge_requests/<iid>` and `merge_request/<id>`; only
	// the latter returns JSON directly.
	endpoint := fmt.Sprintf("%s/api/v4/projects/%s/merge_request/%d", aoneAPIBase, projectID, number)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Enrichment{}, fmt.Errorf("%w: %v", ErrAoneRequestFailed, err)
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return Enrichment{}, fmt.Errorf("%w: %v", ErrAoneRequestFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Drain a small chunk to keep the connection reusable; ignore
		// errors — we already know the request failed.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Enrichment{}, fmt.Errorf("%w: status %d %s", ErrAoneRequestFailed, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Title string `json:"title"`
		State string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Enrichment{}, fmt.Errorf("%w: %v", ErrAoneParseFailed, err)
	}

	title := strings.TrimSpace(payload.Title)
	if title == "" {
		return Enrichment{}, fmt.Errorf("%w: empty title", ErrAoneParseFailed)
	}
	return Enrichment{Title: title, State: mapAoneState(payload.State)}, nil
}

func privateToken() string {
	return strings.TrimSpace(os.Getenv("MULTICA_AONE_PRIVATE_TOKEN"))
}

// mapAoneState normalizes the platform's lifecycle field to the same set
// used for github PRs. Unknown values fall through to "unknown" so the UI
// renders a generic icon rather than dropping the row.
func mapAoneState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "merged":
		return "merged"
	case "closed", "rejected":
		return "closed"
	case "draft":
		return "draft"
	case "accepted", "under_review", "opened", "open", "reviewing":
		return "open"
	default:
		return "unknown"
	}
}
