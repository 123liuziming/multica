package dingtalk

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

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
// Safe to call when both clients are nil/disabled, when the recipient is not
// an Alibaba employee, or when the user lookup fails — every failure is
// logged and swallowed so the inbox flow never blocks on DingTalk.
//
// The HTTP push runs in a goroutine with a fresh 10s timeout so that the
// caller's request context (often cancelled the moment a handler returns)
// does not abort the outbound call.
func PushInbox(ctx context.Context, client *Client, ccClient *CCConnectClient, q InboxRecipientLookup, item db.InboxItem) {
	if !client.Enabled() && !ccClient.Enabled() {
		return
	}
	if ccClient.UsesNotifySessionEndpoint() && !item.IssueID.Valid {
		slog.Debug("dingtalk: skipping non-issue inbox for notify-session",
			"inbox_type", item.Type)
		return
	}
	markdown := BuildInboxMarkdown(item)
	meta := enrichedInboxMetadata(ctx, q, item)
	var skipUserID string

	user, err := q.GetUser(ctx, item.RecipientID)
	if err != nil {
		slog.Warn("dingtalk: lookup recipient failed",
			"inbox_type", item.Type,
			"error", err)
	} else if userID, ok := UserIDFromAlibabaEmail(user.Email); ok {
		skipUserID = userID

		go func(dt *Client, cc *CCConnectClient, dingUserID, title, md, inboxType string, meta map[string]string) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// Prefer cc-connect: it stores context so replies can be linked back to the issue.
			if cc.Enabled() {
				if err := cc.SendNotification(ctx, dingUserID, title, md, meta); err != nil {
					slog.Warn("dingtalk: push inbox via cc-connect failed, falling back to direct",
						"ding_user_id", dingUserID,
						"inbox_type", inboxType,
						"error", err)
					// Fall through to direct DingTalk
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
		}(client, ccClient, userID, item.Title, markdown, item.Type, meta)
	} else {
		slog.Debug("dingtalk: skipping non-alibaba email",
			"inbox_type", item.Type)
	}

	pushAoneLinkedTargets(ctx, client, ccClient, item, skipUserID, meta)
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
