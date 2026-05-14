package dingtalk

import (
	"context"
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
// chat. Safe to call when client is nil/disabled, when the recipient is not
// an Alibaba employee, or when the user lookup fails — every failure is
// logged and swallowed so the inbox flow never blocks on DingTalk.
//
// The HTTP push runs in a goroutine with a fresh 10s timeout so that the
// caller's request context (often cancelled the moment a handler returns)
// does not abort the outbound DingTalk call.
func PushInbox(ctx context.Context, client *Client, q InboxRecipientLookup, item db.InboxItem) {
	if !client.Enabled() {
		return
	}
	user, err := q.GetUser(ctx, item.RecipientID)
	if err != nil {
		slog.Warn("dingtalk: lookup recipient failed",
			"inbox_type", item.Type,
			"error", err)
		return
	}
	userID, ok := UserIDFromAlibabaEmail(user.Email)
	if !ok {
		slog.Debug("dingtalk: skipping non-alibaba email",
			"inbox_type", item.Type)
		return
	}
	markdown := BuildInboxMarkdown(item)
	go func(c *Client, dingUserID, title, md, inboxType string) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.BatchSendOTOMarkdown(ctx, []string{dingUserID}, title, md); err != nil {
			slog.Warn("dingtalk: push inbox failed",
				"ding_user_id", dingUserID,
				"inbox_type", inboxType,
				"error", err)
		}
	}(client, userID, item.Title, markdown, item.Type)
}

// BuildInboxMarkdown renders a Multica inbox item as a DingTalk
// `sampleMarkdown` body. Mirrors the fields surfaced in the in-app inbox
// (title, severity, type, body, details JSON) so a recipient who only
// reads DingTalk gets the same information.
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
		b.WriteString("\n```")
	}
	return b.String()
}
