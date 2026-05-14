package dingtalk

import (
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

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
