package dingtalk

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/notifysummary"
)

// SummaryDispatcher is the per-(staff, issue) notify-summary dispatcher that
// PushInbox optionally hands events to when the workspace enables it. Defined
// as an interface to dodge a hard package dependency in tests; the production
// implementation is *notifysummary.Dispatcher.
type SummaryDispatcher interface {
	Enqueue(staffID, issueID string, settings notifysummary.Settings, n notifysummary.QueuedNotification)
}

// InboxRecipientLookup is the minimal slice of *db.Queries needed to resolve
// an inbox recipient's email. Defined as an interface so call sites can pass
// any queries object (or a fake in tests) without dragging the full Queries
// surface into this package.
type InboxRecipientLookup interface {
	GetUser(ctx context.Context, id pgtype.UUID) (db.User, error)
}

type InboxIssueLookup interface {
	GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error)
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
	ListLabelsByIssue(ctx context.Context, arg db.ListLabelsByIssueParams) ([]db.IssueLabel, error)
	ListPullRequestsByIssue(ctx context.Context, issueID pgtype.UUID) ([]db.ListPullRequestsByIssueRow, error)
}

const issueStatusLabelPrefix = "status:"

// PushInbox best-effort mirrors an inbox item to the recipient's 1:1 DingTalk
// chat. When a CCConnectClient is provided and enabled, the notification is
// routed through cc-connect so it can store context for reply detection.
// Otherwise falls back to the direct DingTalk API.
//
// When the recipient's workspace has notify_summary.enabled = true AND
// dispatcher is non-nil AND the inbox item references an issue:
//
//   1. /notify is called with notify_user=false so cc-connect runs the Aone
//      comment mirror but does NOT deliver the platform-side card.
//   2. The event is enqueued into the per-(staff, issue) summary bucket.
//      When the bucket flushes, the dispatcher renders the workspace's
//      template and POSTs to /notify-session — the user sees one coalesced
//      summary card instead of N raw notifications.
//
// On any failure in the summary path (Aone-leg failure before enqueue) we
// fall back to the direct push so the user isn't left without any signal.
//
// Safe to call when clients/dispatcher are nil/disabled, when the recipient
// is not an Alibaba employee, or when the user lookup fails — every failure
// is logged and swallowed so the inbox flow never blocks.
//
// The HTTP push runs in a goroutine with a fresh 10s timeout so that the
// caller's request context (often cancelled the moment a handler returns)
// does not abort the outbound call.
func PushInbox(ctx context.Context, client *Client, ccClient *CCConnectClient, dispatcher SummaryDispatcher, q InboxRecipientLookup, item db.InboxItem) {
	if !client.Enabled() && !ccClient.Enabled() {
		return
	}
	markdown := BuildInboxMarkdown(item)
	meta := enrichedInboxMetadata(ctx, q, item)
	settings := workspaceNotifySummarySettings(ctx, q, item)
	var skipUserID string

	user, err := q.GetUser(ctx, item.RecipientID)
	if err != nil {
		slog.Warn("dingtalk: lookup recipient failed",
			"inbox_type", item.Type,
			"error", err)
	} else if userID, ok := UserIDFromAlibabaEmail(user.Email); ok {
		skipUserID = userID

		useSummary := settings.Enabled && dispatcher != nil && ccClient.Enabled() && item.IssueID.Valid

		go func(dt *Client, cc *CCConnectClient, disp SummaryDispatcher, dingUserID, title, md, inboxType string, meta map[string]string, summary bool, s notifysummary.Settings, it db.InboxItem) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if summary {
				// Summary path: send to /notify with notify_user=false so only
				// the Aone comment fires, then enqueue for the summary bucket.
				// On Aone-leg failure, fall back to the direct push so the
				// user isn't left without any signal.
				if err := cc.SendNotification(ctx, dingUserID, title, md, meta, false); err == nil {
					disp.Enqueue(dingUserID, formatUUID(it.IssueID), s, notifysummary.QueuedNotification{
						Platform:   "dingtalk",
						UserID:     dingUserID,
						Title:      title,
						Content:    md,
						Metadata:   meta,
						ReceivedAt: time.Now(),
					})
					return
				} else {
					slog.Warn("dingtalk: summary-path /notify (notify_user=false) failed, falling back to direct",
						"ding_user_id", dingUserID,
						"inbox_type", inboxType,
						"error", err)
					// Fall through to direct push below.
				}
			}

			// Direct path: cc-connect first (stores reply context), then
			// fall back to raw DingTalk API.
			if cc.Enabled() {
				if err := cc.SendNotification(ctx, dingUserID, title, md, meta, true); err != nil {
					slog.Warn("dingtalk: push inbox via cc-connect failed, falling back to direct",
						"ding_user_id", dingUserID,
						"inbox_type", inboxType,
						"error", err)
				} else {
					return
				}
			}

			if dt.Enabled() {
				if err := dt.BatchSendOTOMarkdown(ctx, []string{dingUserID}, title, md); err != nil {
					slog.Warn("dingtalk: push inbox failed",
						"ding_user_id", dingUserID,
						"inbox_type", inboxType,
						"error", err)
				}
			}
		}(client, ccClient, dispatcher, userID, item.Title, markdown, item.Type, meta, useSummary, settings, item)
	} else {
		slog.Debug("dingtalk: skipping non-alibaba email",
			"inbox_type", item.Type)
	}

	pushAoneLinkedTargets(ctx, client, ccClient, item, skipUserID, meta)
}

// workspaceNotifySummarySettings reads the per-workspace settings JSONB and
// extracts the notify_summary sub-object. Returns notifysummary.Default()
// when the lookup fails or the key is absent (logged at debug — not a fatal
// condition for the inbox push).
func workspaceNotifySummarySettings(ctx context.Context, q InboxRecipientLookup, item db.InboxItem) notifysummary.Settings {
	issueLookup, ok := q.(InboxIssueLookup)
	if !ok || !item.WorkspaceID.Valid {
		return notifysummary.Default()
	}
	ws, err := issueLookup.GetWorkspace(ctx, item.WorkspaceID)
	if err != nil {
		slog.Debug("dingtalk: lookup workspace for notify_summary settings failed",
			"workspace_id", formatUUID(item.WorkspaceID),
			"error", err)
		return notifysummary.Default()
	}
	s, err := notifysummary.FromWorkspaceSettings(ws.Settings)
	if err != nil {
		slog.Debug("dingtalk: parse notify_summary settings failed",
			"workspace_id", formatUUID(item.WorkspaceID),
			"error", err)
		return notifysummary.Default()
	}
	return s
}

func enrichedInboxMetadata(ctx context.Context, q InboxRecipientLookup, item db.InboxItem) map[string]string {
	meta := inboxMetadata(item)
	var issue db.Issue
	var haveIssue bool
	if issueLookup, ok := q.(InboxIssueLookup); ok && item.IssueID.Valid {
		if iss, err := issueLookup.GetIssue(ctx, item.IssueID); err == nil {
			issue = iss
			haveIssue = true
			if issue.Title != "" {
				meta["issue_title"] = issue.Title
			}
			if issue.Status != "" {
				meta["issue_status"] = issue.Status
			}
			if issue.CreatedAt.Valid {
				meta["issue_create_time"] = issue.CreatedAt.Time.UTC().Format(time.RFC3339)
			}
		} else {
			slog.Debug("dingtalk: lookup issue for inbox metadata failed",
				"issue_id", formatUUID(item.IssueID),
				"error", err)
		}

		if tag := firstStatusLabel(ctx, issueLookup, item); tag != "" {
			meta["issue_status_tag"] = tag
		}

		if urls := pullRequestURLs(ctx, issueLookup, item); urls != "" {
			meta["issue_pull_requests"] = urls
		}

		if haveIssue {
			if ws, err := issueLookup.GetWorkspace(ctx, item.WorkspaceID); err == nil {
				if id := issueIdentifier(ws, issue); id != "" {
					meta["issue_identifier"] = id
					if url := issueURL(ws, id); url != "" {
						meta["issue_url"] = url
					}
				}
				if aoneURL := aoneIssueURL(ws, issue); aoneURL != "" {
					meta["aone_issue_url"] = aoneURL
				}
			} else {
				slog.Debug("dingtalk: lookup workspace for inbox metadata failed",
					"workspace_id", formatUUID(item.WorkspaceID),
					"error", err)
			}
		}
	}
	if item.IssueID.Valid && meta["issue_title"] == "" && item.Title != "" {
		meta["issue_title"] = item.Title
	}
	return meta
}

// issueIdentifier returns the human-readable issue code (e.g. "ACME-101")
// built from the workspace's issue_prefix and the issue number. Returns ""
// when either is missing — fall back to the UUID in that case.
func issueIdentifier(ws db.Workspace, issue db.Issue) string {
	prefix := strings.TrimSpace(ws.IssuePrefix)
	if prefix == "" || issue.Number == 0 {
		return ""
	}
	return fmt.Sprintf("%s-%d", prefix, issue.Number)
}

// issueURL constructs the workspace-scoped web URL for an issue using
// MULTICA_APP_URL. Returns "" when the env var is unset or the slug is
// missing so callers can omit the link instead of producing a broken one.
func issueURL(ws db.Workspace, identifier string) string {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("MULTICA_APP_URL")), "/")
	slug := strings.TrimSpace(ws.Slug)
	if base == "" || slug == "" || identifier == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/issues/%s", base, slug, identifier)
}

// aoneIssueURL constructs the Aone work item URL from the issue title's
// [AONE-<id>] tag and the workspace's aone_project_id setting. Returns ""
// when either piece is missing.
func aoneIssueURL(ws db.Workspace, issue db.Issue) string {
	aoneID := extractAoneID(issue.Title)
	if aoneID == "" {
		return ""
	}
	var settings map[string]any
	if len(ws.Settings) > 0 {
		_ = json.Unmarshal(ws.Settings, &settings)
	}
	projectID, _ := settings["aone_project_id"].(string)
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ""
	}
	return fmt.Sprintf("https://project.aone.alibaba-inc.com/v2/project/%s/req/%s", projectID, aoneID)
}

// firstStatusLabel returns the first label whose name carries the "status:"
// prefix (case-insensitive, after trim). Returns "" when no such label
// exists. The agent uses this as a coarse-grained state hint alongside
// issue.status.
func firstStatusLabel(ctx context.Context, q InboxIssueLookup, item db.InboxItem) string {
	labels, err := q.ListLabelsByIssue(ctx, db.ListLabelsByIssueParams{
		IssueID:     item.IssueID,
		WorkspaceID: item.WorkspaceID,
	})
	if err != nil {
		slog.Debug("dingtalk: list labels for inbox metadata failed",
			"issue_id", formatUUID(item.IssueID),
			"error", err)
		return ""
	}
	for _, l := range labels {
		name := strings.TrimSpace(l.Name)
		if strings.HasPrefix(strings.ToLower(name), issueStatusLabelPrefix) {
			return name
		}
	}
	return ""
}

// pullRequestURLs returns the issue's linked PR / MR URLs (github + aone),
// one per line, in the order returned by ListPullRequestsByIssue (most
// recently created first). Returns "" when the issue has no linked PRs.
func pullRequestURLs(ctx context.Context, q InboxIssueLookup, item db.InboxItem) string {
	prs, err := q.ListPullRequestsByIssue(ctx, item.IssueID)
	if err != nil {
		slog.Debug("dingtalk: list pull requests for inbox metadata failed",
			"issue_id", formatUUID(item.IssueID),
			"error", err)
		return ""
	}
	urls := make([]string, 0, len(prs))
	for _, pr := range prs {
		if u := strings.TrimSpace(pr.HtmlUrl); u != "" {
			urls = append(urls, u)
		}
	}
	return strings.Join(urls, "\n")
}

// BuildInboxMarkdown renders a Multica inbox item as a DingTalk
// `sampleMarkdown` body. Mirrors the fields surfaced in the in-app inbox
// (title, severity, type, body, details JSON) so a recipient who only
// reads DingTalk gets the same information.
//
// A machine-parseable [multica:...] context line is retained as a legacy
// fallback. The authoritative reply context for cc-connect is sent in
// /notify.metadata by PushInbox.
func BuildInboxMarkdown(item db.InboxItem) string {
	var b strings.Builder
	b.WriteString("### Multica · ")
	b.WriteString(item.Title)
	b.WriteString("\n\n")
	b.WriteString("> type: `")
	b.WriteString(item.Type)
	b.WriteString("` · severity: `")
	b.WriteString(item.Severity)
	b.WriteString("`\n\n")
	if item.Body.Valid && strings.TrimSpace(item.Body.String) != "" {
		b.WriteString(item.Body.String)
		b.WriteString("\n\n")
	}
	if len(item.Details) > 0 {
		b.WriteString("```json\n")
		b.Write(item.Details)
		b.WriteString("\n```\n\n")
	}

	wsID := formatUUID(item.WorkspaceID)
	issueID := formatUUID(item.IssueID)
	if wsID != "" && issueID != "" {
		b.WriteString("---\n\n")
		b.WriteString("> Reply to interact with this issue\n\n")
		b.WriteString("[multica:ws=")
		b.WriteString(wsID)
		b.WriteString(",issue=")
		b.WriteString(issueID)
		b.WriteString("]")
	}
	return b.String()
}

func formatUUID(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst)
}
