package dingtalk

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func makeTestUUID(b byte) pgtype.UUID {
	var arr [16]byte
	for i := range arr {
		arr[i] = b
	}
	return pgtype.UUID{Bytes: arr, Valid: true}
}

func TestBuildInboxMarkdownIncludesAllFields(t *testing.T) {
	item := db.InboxItem{
		WorkspaceID: makeTestUUID(0xaa),
		IssueID:     makeTestUUID(0xbb),
		Title:       "Quick create failed",
		Type:        "quick_create_failed",
		Severity:    "action_required",
		Body:        pgtype.Text{String: "agent timed out", Valid: true},
		Details:     []byte(`{"task_id":"abc"}`),
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
		WorkspaceID: makeTestUUID(0xaa),
		IssueID:     makeTestUUID(0xbb),
		Title:       "Issue created",
		Type:        "quick_create_done",
		Severity:    "info",
		Body:        pgtype.Text{}, // not valid
	}
	got := BuildInboxMarkdown(item)
	if strings.Contains(got, "\n\n\n\n") {
		t.Errorf("expected single blank line gap when body is empty:\n%s", got)
	}
	if !strings.Contains(got, "Issue created") {
		t.Errorf("title missing: %s", got)
	}
}

func TestBuildInboxMarkdownIncludesContextFooter(t *testing.T) {
	item := db.InboxItem{
		WorkspaceID: makeTestUUID(0xaa),
		IssueID:     makeTestUUID(0xbb),
		Title:       "Status changed",
		Type:        "status_changed",
		Severity:    "info",
	}
	got := BuildInboxMarkdown(item)
	if !strings.Contains(got, "[multica:ws=aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa,issue=bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb]") {
		t.Errorf("expected context footer with UUIDs, got:\n%s", got)
	}
	if !strings.Contains(got, "Reply to interact with this issue") {
		t.Errorf("expected reply hint, got:\n%s", got)
	}
}

func TestInboxMetadataIncludesReplyContext(t *testing.T) {
	item := db.InboxItem{
		WorkspaceID: makeTestUUID(0xaa),
		IssueID:     makeTestUUID(0xbb),
		Type:        "status_changed",
	}
	got := inboxMetadata(item)
	want := map[string]string{
		"workspace_id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"issue_id":     "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		"inbox_type":   "status_changed",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("metadata[%q] = %q; want %q", k, got[k], v)
		}
	}
}

func TestBuildInboxMarkdownOmitsContextWhenMissingIDs(t *testing.T) {
	item := db.InboxItem{
		Title:    "No IDs",
		Type:     "quick_create_done",
		Severity: "info",
	}
	got := BuildInboxMarkdown(item)
	if strings.Contains(got, "[multica:") {
		t.Errorf("expected no context footer when IDs missing, got:\n%s", got)
	}
}
