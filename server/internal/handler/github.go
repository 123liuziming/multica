package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/aone"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ── Response shapes ─────────────────────────────────────────────────────────

type GitHubInstallationResponse struct {
	ID               string  `json:"id"`
	WorkspaceID      string  `json:"workspace_id"`
	InstallationID   int64   `json:"installation_id"`
	AccountLogin     string  `json:"account_login"`
	AccountType      string  `json:"account_type"`
	AccountAvatarURL *string `json:"account_avatar_url"`
	CreatedAt        string  `json:"created_at"`
}

// PullRequestResponse is the source-agnostic shape returned for both
// github and aone PRs. repo_owner/repo_name/number/pr_created_at/
// pr_updated_at are nullable because the aone table allows them to be
// absent (a future external-source extension would too).
type PullRequestResponse struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	Source          string  `json:"source"` // "github" | "aone"
	RepoOwner       *string `json:"repo_owner"`
	RepoName        *string `json:"repo_name"`
	Number          *int32  `json:"number"`
	Title           string  `json:"title"`
	State           string  `json:"state"`
	HtmlURL         string  `json:"html_url"`
	Branch          *string `json:"branch"`
	AuthorLogin     *string `json:"author_login"`
	AuthorAvatarURL *string `json:"author_avatar_url"`
	MergedAt        *string `json:"merged_at"`
	ClosedAt        *string `json:"closed_at"`
	PRCreatedAt     *string `json:"pr_created_at"`
	PRUpdatedAt     *string `json:"pr_updated_at"`
}

type GitHubConnectResponse struct {
	URL       string `json:"url"`
	Configured bool  `json:"configured"`
}

func githubInstallationToResponse(i db.GithubInstallation) GitHubInstallationResponse {
	return GitHubInstallationResponse{
		ID:               uuidToString(i.ID),
		WorkspaceID:      uuidToString(i.WorkspaceID),
		InstallationID:   i.InstallationID,
		AccountLogin:     i.AccountLogin,
		AccountType:      i.AccountType,
		AccountAvatarURL: textToPtr(i.AccountAvatarUrl),
		CreatedAt:        timestampToString(i.CreatedAt),
	}
}

func githubPullRequestToResponse(p db.GithubPullRequest) PullRequestResponse {
	prCreated := timestampToString(p.PrCreatedAt)
	prUpdated := timestampToString(p.PrUpdatedAt)
	owner, repo, num := p.RepoOwner, p.RepoName, p.PrNumber
	return PullRequestResponse{
		ID:              uuidToString(p.ID),
		WorkspaceID:     uuidToString(p.WorkspaceID),
		Source:          "github",
		RepoOwner:       &owner,
		RepoName:        &repo,
		Number:          &num,
		Title:           p.Title,
		State:           p.State,
		HtmlURL:         p.HtmlUrl,
		Branch:          textToPtr(p.Branch),
		AuthorLogin:     textToPtr(p.AuthorLogin),
		AuthorAvatarURL: textToPtr(p.AuthorAvatarUrl),
		MergedAt:        timestampToPtr(p.MergedAt),
		ClosedAt:        timestampToPtr(p.ClosedAt),
		PRCreatedAt:     &prCreated,
		PRUpdatedAt:     &prUpdated,
	}
}

func aonePullRequestToResponse(p db.AonePullRequest) PullRequestResponse {
	return PullRequestResponse{
		ID:              uuidToString(p.ID),
		WorkspaceID:     uuidToString(p.WorkspaceID),
		Source:          "aone",
		RepoOwner:       textToPtr(p.RepoOwner),
		RepoName:        textToPtr(p.RepoName),
		Number:          int4ToPtr(p.PrNumber),
		Title:           p.Title,
		State:           p.State,
		HtmlURL:         p.HtmlUrl,
		Branch:          nil,
		AuthorLogin:     textToPtr(p.AuthorLogin),
		AuthorAvatarURL: textToPtr(p.AuthorAvatarUrl),
		MergedAt:        timestampToPtr(p.MergedAt),
		ClosedAt:        timestampToPtr(p.ClosedAt),
		PRCreatedAt:     timestampToPtr(p.PrCreatedAt),
		PRUpdatedAt:     timestampToPtr(p.PrUpdatedAt),
	}
}

// pullRequestRowToResponse maps the UNION row produced by
// ListPullRequestsByIssue (where repo_owner / repo_name / pr_number etc.
// are nullable because the aone branch dominates the inferred type).
func pullRequestRowToResponse(r db.ListPullRequestsByIssueRow) PullRequestResponse {
	return PullRequestResponse{
		ID:              uuidToString(r.ID),
		WorkspaceID:     uuidToString(r.WorkspaceID),
		Source:          r.Source,
		RepoOwner:       textToPtr(r.RepoOwner),
		RepoName:        textToPtr(r.RepoName),
		Number:          int4ToPtr(r.PrNumber),
		Title:           r.Title,
		State:           r.State,
		HtmlURL:         r.HtmlUrl,
		Branch:          textToPtr(r.Branch),
		AuthorLogin:     textToPtr(r.AuthorLogin),
		AuthorAvatarURL: textToPtr(r.AuthorAvatarUrl),
		MergedAt:        timestampToPtr(r.MergedAt),
		ClosedAt:        timestampToPtr(r.ClosedAt),
		PRCreatedAt:     timestampToPtr(r.PrCreatedAt),
		PRUpdatedAt:     timestampToPtr(r.PrUpdatedAt),
	}
}

// ── Connect / state token ───────────────────────────────────────────────────

// githubAppSlug returns the GitHub App slug used to build the install URL.
// Empty when the integration is not configured for this deployment.
func githubAppSlug() string { return strings.TrimSpace(os.Getenv("GITHUB_APP_SLUG")) }

// githubWebhookSecret is shared by webhook verification and state-token signing.
// We reuse the webhook secret as the state HMAC key so operators only need to
// configure one value.
func githubWebhookSecret() string { return strings.TrimSpace(os.Getenv("GITHUB_WEBHOOK_SECRET")) }

// isGitHubConfigured returns true only when BOTH the install slug and the
// webhook secret are set. The Connect button uses this single flag, so the
// frontend never offers a flow that the backend would reject.
func isGitHubConfigured() bool { return githubAppSlug() != "" && githubWebhookSecret() != "" }

// signState produces an opaque token that binds a workspace ID to the
// install flow so the setup callback can recover the workspace without
// trusting query params alone. Format: "<workspaceID>.<nonce>.<sigHex>".
func signState(workspaceID string) (string, error) {
	secret := githubWebhookSecret()
	if secret == "" {
		return "", errors.New("github integration is not configured")
	}
	nonceBytes := make([]byte, 12)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(nonceBytes)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(workspaceID))
	mac.Write([]byte("."))
	mac.Write([]byte(nonce))
	sig := hex.EncodeToString(mac.Sum(nil))
	return workspaceID + "." + nonce + "." + sig, nil
}

func verifyState(token string) (string, bool) {
	secret := githubWebhookSecret()
	if secret == "" {
		return "", false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", false
	}
	workspaceID, nonce, sig := parts[0], parts[1], parts[2]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(workspaceID))
	mac.Write([]byte("."))
	mac.Write([]byte(nonce))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return "", false
	}
	return workspaceID, true
}

// GitHubConnect (GET /api/workspaces/{id}/github/connect) returns the URL the
// browser should open to install the Multica GitHub App against the caller's
// repos. The state token binds the resulting setup callback to this workspace.
func (h *Handler) GitHubConnect(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id"); !ok {
		return
	}
	if !isGitHubConfigured() {
		writeJSON(w, http.StatusOK, GitHubConnectResponse{Configured: false})
		return
	}
	slug := githubAppSlug()
	state, err := signState(workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign state")
		return
	}
	installURL := fmt.Sprintf(
		"https://github.com/apps/%s/installations/new?state=%s",
		url.PathEscape(slug),
		url.QueryEscape(state),
	)
	writeJSON(w, http.StatusOK, GitHubConnectResponse{URL: installURL, Configured: true})
}

// GitHubSetupCallback (GET /api/github/setup) handles the redirect GitHub
// sends after a user installs (or re-authorizes) the App. We expect
// ?installation_id=<id>&state=<signed token>. We persist the installation
// row (workspace ↔ installation_id mapping), then bounce the user back to
// the Settings → Integrations page in the web app.
func (h *Handler) GitHubSetupCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	installationIDStr := q.Get("installation_id")
	state := q.Get("state")
	frontend := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if frontend == "" {
		frontend = "http://localhost:3000"
	}
	settingsURL := strings.TrimRight(frontend, "/") + "/settings"

	if installationIDStr == "" || state == "" {
		http.Redirect(w, r, settingsURL+"?github_error=missing_params", http.StatusFound)
		return
	}
	workspaceID, ok := verifyState(state)
	if !ok {
		http.Redirect(w, r, settingsURL+"?github_error=invalid_state", http.StatusFound)
		return
	}
	installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
	if err != nil {
		http.Redirect(w, r, settingsURL+"?github_error=bad_installation_id", http.StatusFound)
		return
	}
	wsUUID, err := parseStrictUUID(workspaceID)
	if err != nil {
		http.Redirect(w, r, settingsURL+"?github_error=bad_workspace", http.StatusFound)
		return
	}
	// Resolve the installation against GitHub's API to capture display info.
	// If the App auth is not configured we still create the row with the
	// minimum we know; webhook events will refresh it as soon as one fires.
	login, accountType, avatar := fetchInstallationAccount(r.Context(), installationID)

	// Best-effort capture of the connecting user (may be nil if the public
	// callback was hit without a session — e.g. user wasn't logged in to
	// Multica when they finished the GitHub install). Either way we save
	// the row so the workspace owner sees the connection on next reload.
	connectedBy := pgtype.UUID{}
	if userID := requestUserID(r); userID != "" {
		if u, err := parseStrictUUID(userID); err == nil {
			connectedBy = u
		}
	}

	inst, err := h.Queries.CreateGitHubInstallation(r.Context(), db.CreateGitHubInstallationParams{
		WorkspaceID:      wsUUID,
		InstallationID:   installationID,
		AccountLogin:     login,
		AccountType:      accountType,
		AccountAvatarUrl: ptrToText(avatar),
		ConnectedByID:    connectedBy,
	})
	if err != nil {
		slog.Error("github: failed to persist installation", "err", err, "installation_id", installationID)
		http.Redirect(w, r, settingsURL+"?github_error=persist_failed", http.StatusFound)
		return
	}
	h.publish(protocol.EventGitHubInstallationCreated, workspaceID, "system", "", map[string]any{
		"installation": githubInstallationToResponse(inst),
	})
	http.Redirect(w, r, settingsURL+"?github_connected=1", http.StatusFound)
}

// fetchInstallationAccount tries to enrich the installation row with the
// account name + avatar via GitHub's public API. We deliberately do NOT
// require GitHub App JWT auth here — the install endpoint is publicly
// readable for installations on public accounts, and on failure we fall
// back to placeholders that the next webhook will overwrite.
func fetchInstallationAccount(ctx context.Context, installationID int64) (login, accountType string, avatar *string) {
	login = "unknown"
	accountType = "User"
	avatar = nil
	url := fmt.Sprintf("https://api.github.com/app/installations/%d", installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var body struct {
		Account struct {
			Login     string `json:"login"`
			Type      string `json:"type"`
			AvatarURL string `json:"avatar_url"`
		} `json:"account"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return
	}
	if body.Account.Login != "" {
		login = body.Account.Login
	}
	if body.Account.Type != "" {
		accountType = body.Account.Type
	}
	if body.Account.AvatarURL != "" {
		v := body.Account.AvatarURL
		avatar = &v
	}
	return
}

// ── Listing / disconnect ────────────────────────────────────────────────────

func (h *Handler) ListGitHubInstallations(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListGitHubInstallationsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list installations")
		return
	}
	out := make([]GitHubInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, githubInstallationToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"installations": out, "configured": isGitHubConfigured()})
}

func (h *Handler) DeleteGitHubInstallation(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	id := chi.URLParam(r, "installationId")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "installation id")
	if !ok {
		return
	}
	if err := h.Queries.DeleteGitHubInstallation(r.Context(), db.DeleteGitHubInstallationParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove installation")
		return
	}
	h.publish(protocol.EventGitHubInstallationDeleted, workspaceID, "system", "", map[string]any{
		"id": id,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ── List / Link / Unlink PRs for an issue ──────────────────────────────────

func (h *Handler) ListPullRequestsForIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListPullRequestsByIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pull requests")
		return
	}
	out := make([]PullRequestResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, pullRequestRowToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"pull_requests": out})
}

// LinkPullRequestToIssue (POST /api/issues/{id}/pull-requests) attaches a
// PR URL to an issue. The URL parser decides whether the PR lives in
// github_pull_request or aone_pull_request and we route the upsert
// accordingly. The user-supplied title (optional) wins over enrichment
// which wins over the derived "owner/repo#N" hostname-style fallback.
func (h *Handler) LinkPullRequestToIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	var body struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	parsed, err := parsePullRequestURL(body.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	ctx := r.Context()
	userTitle := strings.TrimSpace(body.Title)

	var resp PullRequestResponse
	var prID pgtype.UUID

	switch parsed.Source {
	case prSourceAone:
		title := userTitle
		state := "unknown"
		enriched, enrichOK := h.enrichAonePullRequest(ctx, parsed)
		if enrichOK {
			if title == "" {
				title = enriched.Title
			}
			state = enriched.State
		}
		if title == "" {
			title = parsed.DerivedTitle
		}

		params := db.UpsertAonePullRequestByURLParams{
			WorkspaceID: issue.WorkspaceID,
			HtmlUrl:     parsed.HTMLURL,
			Title:       title,
			State:       state,
			RepoOwner:   ptrToText(parsed.RepoOwner),
			RepoName:    ptrToText(parsed.RepoName),
			PrNumber:    ptrToInt4(parsed.Number),
		}
		if enrichOK {
			params.LastEnrichedAt = pgtype.Timestamptz{Time: nowUTC(), Valid: true}
		}
		pr, err := h.Queries.UpsertAonePullRequestByURL(ctx, params)
		if err != nil {
			slog.Warn("link pr: upsert aone failed", "err", err)
			writeError(w, http.StatusInternalServerError, "failed to save pull request")
			return
		}
		prID = pr.ID
		resp = aonePullRequestToResponse(pr)

	default:
		writeError(w, http.StatusBadRequest, "unsupported pull request source")
		return
	}

	if err := h.Queries.LinkIssueToPullRequest(ctx, db.LinkIssueToPullRequestParams{
		IssueID:       issue.ID,
		PullRequestID: prID,
		Source:        resp.Source,
		LinkedByType:  strToText("user"),
		LinkedByID:    pgtype.UUID{Bytes: userUUID.Bytes, Valid: userUUID.Valid},
	}); err != nil {
		slog.Warn("link pr: link failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to link pull request")
		return
	}

	h.publish(protocol.EventPullRequestLinked, workspaceID, "user", userID, map[string]any{
		"issue_id":     uuidToString(issue.ID),
		"pull_request": resp,
	})
	writeJSON(w, http.StatusCreated, map[string]any{"pull_request": resp})
}

// UnlinkPullRequestFromIssue (DELETE /api/issues/{id}/pull-requests/{source}/{prId})
// drops the join-table row but keeps the PR record (matches DetachLabel —
// the PR may still be useful from other issues or for history). Both the
// source path segment and the PR UUID are bounded to this workspace so a
// guessed cross-tenant prId returns 404.
func (h *Handler) UnlinkPullRequestFromIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	source := chi.URLParam(r, "source")
	if source != "github" && source != "aone" {
		writeError(w, http.StatusBadRequest, "unsupported pull request source")
		return
	}
	prUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "prId"), "prId")
	if !ok {
		return
	}

	ctx := r.Context()
	switch source {
	case "github":
		if _, err := h.Queries.GetGitHubPullRequestByID(ctx, db.GetGitHubPullRequestByIDParams{
			ID:          prUUID,
			WorkspaceID: issue.WorkspaceID,
		}); err != nil {
			writeError(w, http.StatusNotFound, "pull request not found")
			return
		}
	case "aone":
		if _, err := h.Queries.GetAonePullRequestByID(ctx, db.GetAonePullRequestByIDParams{
			ID:          prUUID,
			WorkspaceID: issue.WorkspaceID,
		}); err != nil {
			writeError(w, http.StatusNotFound, "pull request not found")
			return
		}
	}

	if err := h.Queries.UnlinkIssueFromPullRequest(ctx, db.UnlinkIssueFromPullRequestParams{
		IssueID:       issue.ID,
		PullRequestID: prUUID,
		Source:        source,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlink pull request")
		return
	}

	h.publish(protocol.EventPullRequestUnlinked, uuidToString(issue.WorkspaceID), "user", userID, map[string]any{
		"issue_id":        uuidToString(issue.ID),
		"pull_request_id": uuidToString(prUUID),
		"source":          source,
	})
	w.WriteHeader(http.StatusNoContent)
}

// enrichAonePullRequest is a thin wrapper around aone.Enrich that swallows
// the "a1 not installed" / generic enrichment errors so the link handler
// can still create the row. We log unexpected errors so operators learn
// about a flaky a1 install but never block the link.
func (h *Handler) enrichAonePullRequest(ctx context.Context, p parsedPullRequestURL) (aone.Enrichment, bool) {
	if p.RepoOwner == nil || p.RepoName == nil || p.Number == nil {
		return aone.Enrichment{}, false
	}
	e, err := aone.Enrich(ctx, *p.RepoOwner, *p.RepoName, *p.Number)
	if err != nil {
		if !errors.Is(err, aone.ErrAoneNotConfigured) {
			slog.Warn("aone enrich failed", "err", err, "url", p.HTMLURL)
		}
		return aone.Enrichment{}, false
	}
	return e, true
}

func nowUTC() time.Time { return time.Now().UTC() }

// ── Webhook ─────────────────────────────────────────────────────────────────

// identifierRe extracts identifiers like "MUL-1510" from text. Case-insensitive
// because branch names are conventionally lowercase but issue prefixes are
// uppercase. Word boundary on the left prevents matching inside email-style
// strings (e.g. "abc@MUL-1") and the digit anchor on the right rules out
// version numbers like "v1.2-3".
var identifierRe = regexp.MustCompile(`(?i)\b([a-z][a-z0-9]{1,9})-(\d+)\b`)

// HandleGitHubWebhook (POST /api/webhooks/github) is GitHub's destination for
// every event from a connected installation. We verify HMAC signature, route
// on X-GitHub-Event, and either upsert PR rows + auto-link to issues or
// remove the installation on uninstall.
func (h *Handler) HandleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10 MiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}
	secret := githubWebhookSecret()
	if secret == "" {
		// Refusing to process webhooks at all is safer than treating an
		// unconfigured deployment as "all signatures valid".
		writeError(w, http.StatusServiceUnavailable, "github webhooks not configured")
		return
	}
	sigHeader := r.Header.Get("X-Hub-Signature-256")
	if !verifyWebhookSignature(secret, sigHeader, body) {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}
	event := r.Header.Get("X-GitHub-Event")
	ctx := r.Context()
	switch event {
	case "ping":
		writeJSON(w, http.StatusOK, map[string]string{"ok": "pong"})
		return
	case "installation":
		h.handleInstallationEvent(ctx, body)
	case "pull_request":
		h.handlePullRequestEvent(ctx, body)
	default:
		// Acknowledge every event so GitHub doesn't mark the endpoint failing,
		// but ignore types we don't model.
	}
	w.WriteHeader(http.StatusAccepted)
}

func verifyWebhookSignature(secret, header string, body []byte) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}

type ghInstallationPayload struct {
	Action       string `json:"action"`
	Installation struct {
		ID      int64 `json:"id"`
		Account struct {
			Login     string `json:"login"`
			Type      string `json:"type"`
			AvatarURL string `json:"avatar_url"`
		} `json:"account"`
	} `json:"installation"`
}

func (h *Handler) handleInstallationEvent(ctx context.Context, body []byte) {
	var p ghInstallationPayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Warn("github: bad installation payload", "err", err)
		return
	}
	switch p.Action {
	case "deleted", "suspend":
		// User removed the App on GitHub — drop our row so the workspace
		// stops trusting this installation_id. We DELETE … RETURNING so
		// the broadcast can be scoped to the right workspace; events
		// without WorkspaceID are dropped by the realtime listener and
		// would leave already-open Settings tabs stale.
		deleted, err := h.Queries.DeleteGitHubInstallationByInstallationID(ctx, p.Installation.ID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return // already gone — nothing to broadcast
			}
			slog.Warn("github: delete installation failed", "err", err, "installation_id", p.Installation.ID)
			return
		}
		h.publish(protocol.EventGitHubInstallationDeleted, uuidToString(deleted.WorkspaceID), "system", "", map[string]any{
			"installation_id": p.Installation.ID,
			"id":              uuidToString(deleted.ID),
		})
	case "created", "new_permissions_accepted", "unsuspend":
		// We don't know which workspace this maps to from the webhook
		// alone — the setup callback handler is what binds installation
		// to workspace, so we just refresh metadata if we already have
		// a row.
		existing, err := h.Queries.GetGitHubInstallationByInstallationID(ctx, p.Installation.ID)
		if err != nil {
			return
		}
		avatar := p.Installation.Account.AvatarURL
		_, err = h.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
			WorkspaceID:      existing.WorkspaceID,
			InstallationID:   p.Installation.ID,
			AccountLogin:     p.Installation.Account.Login,
			AccountType:      coalesce(p.Installation.Account.Type, "User"),
			AccountAvatarUrl: ptrToText(strPtrOrNil(avatar)),
			ConnectedByID:    existing.ConnectedByID,
		})
		if err != nil {
			slog.Warn("github: refresh installation failed", "err", err)
		}
	}
}

type ghPullRequestPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number    int32  `json:"number"`
		HTMLURL   string `json:"html_url"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		State     string `json:"state"`
		Draft     bool   `json:"draft"`
		Merged    bool   `json:"merged"`
		MergedAt  string `json:"merged_at"`
		ClosedAt  string `json:"closed_at"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
		Head      struct {
			Ref string `json:"ref"`
		} `json:"head"`
		User struct {
			Login     string `json:"login"`
			AvatarURL string `json:"avatar_url"`
		} `json:"user"`
	} `json:"pull_request"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

func (h *Handler) handlePullRequestEvent(ctx context.Context, body []byte) {
	var p ghPullRequestPayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Warn("github: bad pull_request payload", "err", err)
		return
	}
	if p.Installation.ID == 0 {
		return
	}
	inst, err := h.Queries.GetGitHubInstallationByInstallationID(ctx, p.Installation.ID)
	if err != nil {
		// Webhook from an installation we never wired up — nothing we
		// can attribute to a workspace, so drop it silently.
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("github: lookup installation failed", "err", err)
		}
		return
	}

	state := derivePRState(p.PullRequest.State, p.PullRequest.Draft, p.PullRequest.Merged)
	pr, err := h.Queries.UpsertGitHubPullRequest(ctx, db.UpsertGitHubPullRequestParams{
		WorkspaceID:      inst.WorkspaceID,
		InstallationID:   inst.InstallationID,
		RepoOwner:        p.Repository.Owner.Login,
		RepoName:         p.Repository.Name,
		PrNumber:         p.PullRequest.Number,
		Title:            p.PullRequest.Title,
		State:            state,
		HtmlUrl:          p.PullRequest.HTMLURL,
		Branch:           ptrToText(strPtrOrNil(p.PullRequest.Head.Ref)),
		AuthorLogin:      ptrToText(strPtrOrNil(p.PullRequest.User.Login)),
		AuthorAvatarUrl:  ptrToText(strPtrOrNil(p.PullRequest.User.AvatarURL)),
		MergedAt:         parseGHTime(p.PullRequest.MergedAt),
		ClosedAt:         parseGHTime(p.PullRequest.ClosedAt),
		PrCreatedAt:      parseGHTimeRequired(p.PullRequest.CreatedAt),
		PrUpdatedAt:      parseGHTimeRequired(p.PullRequest.UpdatedAt),
	})
	if err != nil {
		slog.Warn("github: upsert pr failed", "err", err)
		return
	}

	workspaceID := uuidToString(inst.WorkspaceID)
	resp := githubPullRequestToResponse(pr)

	// Auto-link: scan title/body/branch for issue identifiers, look them
	// up in this workspace, attach the link rows. Idempotent (ON CONFLICT
	// DO NOTHING) so re-firing the webhook doesn't duplicate.
	idents := extractIdentifiers(p.PullRequest.Title, p.PullRequest.Body, p.PullRequest.Head.Ref)
	prefix := h.getIssuePrefix(ctx, inst.WorkspaceID)
	linkedIssueIDs := make([]string, 0, len(idents))
	for _, id := range idents {
		issue, ok := h.lookupIssueByIdentifier(ctx, inst.WorkspaceID, prefix, id)
		if !ok {
			continue
		}
		if err := h.Queries.LinkIssueToPullRequest(ctx, db.LinkIssueToPullRequestParams{
			IssueID:       issue.ID,
			PullRequestID: pr.ID,
			Source:        "github",
			LinkedByType:  strToText("system"),
			LinkedByID:    pgtype.UUID{},
		}); err != nil {
			slog.Warn("github: link failed", "err", err)
			continue
		}
		linkedIssueIDs = append(linkedIssueIDs, uuidToString(issue.ID))

		// A terminal PR event (`merged` or `closed`) may be the moment the
		// last in-flight sibling resolves, so we re-evaluate the issue on
		// both. We advance the issue to done when:
		//   1. the issue isn't already terminal (`done` / `cancelled`);
		//   2. no sibling PR is still `open` / `draft`;
		//   3. at least one linked PR (this one or a sibling) is `merged`.
		// Rule (3) prevents an "all closed-without-merge" sequence from
		// silently auto-closing the issue — if nothing was ever delivered,
		// the user should decide what to do manually.
		if (state == "merged" || state == "closed") && issue.Status != "done" && issue.Status != "cancelled" {
			counts, err := h.Queries.GetSiblingPullRequestStateCountsForIssue(ctx, db.GetSiblingPullRequestStateCountsForIssueParams{
				IssueID: issue.ID,
				ID:      pr.ID,
			})
			if err != nil {
				slog.Warn("github: count sibling pr states failed", "err", err, "issue_id", uuidToString(issue.ID))
				continue
			}
			anyMerged := state == "merged" || counts.MergedCount > 0
			if counts.OpenCount == 0 && anyMerged {
				h.advanceIssueToDone(ctx, issue, workspaceID)
			}
		}
	}

	// Broadcast PR change to the workspace so any open issue detail page
	// re-queries its PR list.
	h.publish(protocol.EventPullRequestUpdated, workspaceID, "system", "", map[string]any{
		"pull_request": resp,
		"linked_issue_ids": linkedIssueIDs,
	})
}

func derivePRState(state string, draft, merged bool) string {
	if merged {
		return "merged"
	}
	if state == "closed" {
		return "closed"
	}
	if draft {
		return "draft"
	}
	return "open"
}

func parseGHTime(s string) pgtype.Timestamptz {
	if s == "" {
		return pgtype.Timestamptz{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func parseGHTimeRequired(s string) pgtype.Timestamptz {
	t := parseGHTime(s)
	if !t.Valid {
		return pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	}
	return t
}

// extractIdentifiers pulls every "PREFIX-NUMBER" match across the supplied
// fields, deduplicating in input order.
func extractIdentifiers(parts ...string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, src := range parts {
		for _, m := range identifierRe.FindAllStringSubmatch(src, -1) {
			ident := strings.ToUpper(m[1]) + "-" + m[2]
			if _, dup := seen[ident]; dup {
				continue
			}
			seen[ident] = struct{}{}
			out = append(out, ident)
		}
	}
	return out
}

// lookupIssueByIdentifier looks up an issue in the given workspace by its
// "PREFIX-NUMBER" identifier. Returns the row + true if the prefix matches
// the workspace's configured prefix and the number resolves to a real issue.
func (h *Handler) lookupIssueByIdentifier(ctx context.Context, workspaceID pgtype.UUID, prefix, identifier string) (db.Issue, bool) {
	idx := strings.LastIndex(identifier, "-")
	if idx < 0 {
		return db.Issue{}, false
	}
	gotPrefix, numStr := identifier[:idx], identifier[idx+1:]
	if !strings.EqualFold(gotPrefix, prefix) {
		return db.Issue{}, false
	}
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return db.Issue{}, false
	}
	issue, err := h.Queries.GetIssueByNumber(ctx, db.GetIssueByNumberParams{
		WorkspaceID: workspaceID,
		Number:      int32(n),
	})
	if err != nil {
		return db.Issue{}, false
	}
	return issue, true
}

func (h *Handler) advanceIssueToDone(ctx context.Context, issue db.Issue, workspaceID string) {
	updated, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:     issue.ID,
		Status: "done",
	})
	if err != nil {
		slog.Warn("github: advance issue to done failed", "err", err)
		return
	}
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	resp := issueToResponse(updated, prefix)
	h.publish(protocol.EventIssueUpdated, workspaceID, "system", "", map[string]any{
		"issue":          resp,
		"status_changed": true,
		"prev_status":    issue.Status,
		"creator_type":   issue.CreatorType,
		"creator_id":     uuidToString(issue.CreatorID),
		"source":         "github_pr_merged",
	})
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func parseStrictUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	return u, nil
}

func coalesce(a, fallback string) string {
	if strings.TrimSpace(a) == "" {
		return fallback
	}
	return a
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}
