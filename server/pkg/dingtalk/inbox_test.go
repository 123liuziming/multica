package dingtalk

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestBuildInboxMarkdownIncludesAllFields(t *testing.T) {
	item := db.InboxItem{
		Title:    "Quick create failed",
		Type:     "quick_create_failed",
		Severity: "action_required",
		Body:     pgtype.Text{String: "agent timed out", Valid: true},
		Details:  []byte(`{"task_id":"abc"}`),
	}
	got := BuildInboxMarkdown(item)
	for _, want := range []string{
		"Quick create failed",
		"`quick_create_failed`",
		"`action_required`",
		"agent timed out",
		`"task_id":"abc"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("markdown missing %q\n--- got ---\n%s", want, got)
		}
	}
}

func TestBuildInboxMarkdownOmitsEmptyBody(t *testing.T) {
	item := db.InboxItem{
		Title:    "Issue created",
		Type:     "quick_create_done",
		Severity: "info",
		Body:     pgtype.Text{}, // not valid
	}
	got := BuildInboxMarkdown(item)
	if strings.Contains(got, "\n\n\n\n") {
		t.Errorf("expected single blank line gap when body is empty:\n%s", got)
	}
	if !strings.Contains(got, "Issue created") {
		t.Errorf("title missing: %s", got)
	}
}
