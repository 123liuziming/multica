package dingtalk

import (
	"context"
	"encoding/hex"
	"log/slog"
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
	markdown := BuildInboxMarkdown(item)
	meta := inboxMetadata(item)
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

	pushAoneLinkedTargets(ctx, client, ccClient, item, skipUserID)
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
