package dingtalk

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

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

type fakeInboxMetadataLookup struct {
	user      db.User
	issue     db.Issue
	workspace db.Workspace
	labels    []db.IssueLabel
	prs       []db.ListPullRequestsByIssueRow
}

func (f fakeInboxMetadataLookup) GetUser(context.Context, pgtype.UUID) (db.User, error) {
	return f.user, nil
}

func (f fakeInboxMetadataLookup) GetIssue(context.Context, pgtype.UUID) (db.Issue, error) {
	return f.issue, nil
}

func (f fakeInboxMetadataLookup) GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error) {
	return f.workspace, nil
}

func (f fakeInboxMetadataLookup) ListLabelsByIssue(context.Context, db.ListLabelsByIssueParams) ([]db.IssueLabel, error) {
	return f.labels, nil
}

func (f fakeInboxMetadataLookup) ListPullRequestsByIssue(context.Context, pgtype.UUID) ([]db.ListPullRequestsByIssueRow, error) {
	return f.prs, nil
}

func TestEnrichedInboxMetadataIncludesIssueFields(t *testing.T) {
	createdAt := time.Date(2026, 5, 27, 9, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	issueID := makeTestUUID(0xbb)
	item := db.InboxItem{
		WorkspaceID: makeTestUUID(0xaa),
		IssueID:     issueID,
		Title:       "Notification title",
		Type:        "status_changed",
	}
	t.Setenv("MULTICA_APP_URL", "https://multica.example.com/")
	got := enrichedInboxMetadata(context.Background(), fakeInboxMetadataLookup{
		issue: db.Issue{
			ID:        issueID,
			Title:     "Fix deploy",
			Status:    "in_progress",
			Number:    101,
			CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
		},
		workspace: db.Workspace{
			Slug:        "shuizhao-gh",
			IssuePrefix: "SHUIZHAO-GH",
		},
		labels: []db.IssueLabel{
			{Name: "area:backend"},
			{Name: "Status:Code-Review"},
			{Name: "status:blocked"},
		},
		prs: []db.ListPullRequestsByIssueRow{
			{HtmlUrl: "https://code.alibaba-inc.com/multica/server/codereview/12345"},
			{HtmlUrl: "https://github.com/multica-ai/multica/pull/42"},
		},
	}, item)

	if got["issue_title"] != "Fix deploy" {
		t.Errorf("issue_title metadata = %q; want Fix deploy", got["issue_title"])
	}
	if got["issue_status"] != "in_progress" {
		t.Errorf("issue_status metadata = %q; want in_progress", got["issue_status"])
	}
	if got["issue_create_time"] != "2026-05-27T01:30:00Z" {
		t.Errorf("issue_create_time metadata = %q", got["issue_create_time"])
	}
	// First label matching status: prefix wins, regardless of casing.
	if got["issue_status_tag"] != "Status:Code-Review" {
		t.Errorf("issue_status_tag metadata = %q; want Status:Code-Review", got["issue_status_tag"])
	}
	wantPRs := "https://code.alibaba-inc.com/multica/server/codereview/12345\nhttps://github.com/multica-ai/multica/pull/42"
	if got["issue_pull_requests"] != wantPRs {
		t.Errorf("issue_pull_requests metadata = %q; want %q", got["issue_pull_requests"], wantPRs)
	}
	if got["issue_identifier"] != "SHUIZHAO-GH-101" {
		t.Errorf("issue_identifier metadata = %q; want SHUIZHAO-GH-101", got["issue_identifier"])
	}
	if got["issue_url"] != "https://multica.example.com/shuizhao-gh/issues/SHUIZHAO-GH-101" {
		t.Errorf("issue_url metadata = %q", got["issue_url"])
	}
}

func TestEnrichedInboxMetadataOmitsURLWhenAppURLUnset(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "")
	issueID := makeTestUUID(0xbb)
	item := db.InboxItem{
		WorkspaceID: makeTestUUID(0xaa),
		IssueID:     issueID,
		Title:       "Notification title",
		Type:        "status_changed",
	}
	got := enrichedInboxMetadata(context.Background(), fakeInboxMetadataLookup{
		issue: db.Issue{ID: issueID, Title: "x", Status: "todo", Number: 7},
		workspace: db.Workspace{
			Slug:        "shuizhao-gh",
			IssuePrefix: "SHUIZHAO-GH",
		},
	}, item)
	if got["issue_identifier"] != "SHUIZHAO-GH-7" {
		t.Errorf("identifier should still be set even without app url: got %q", got["issue_identifier"])
	}
	if _, present := got["issue_url"]; present {
		t.Errorf("issue_url should be omitted when MULTICA_APP_URL is unset; got %q", got["issue_url"])
	}
}

func TestEnrichedInboxMetadataOmitsStatusTagAndPRsWhenAbsent(t *testing.T) {
	issueID := makeTestUUID(0xbb)
	item := db.InboxItem{
		WorkspaceID: makeTestUUID(0xaa),
		IssueID:     issueID,
		Title:       "Notification title",
		Type:        "status_changed",
	}
	got := enrichedInboxMetadata(context.Background(), fakeInboxMetadataLookup{
		issue: db.Issue{
			ID:     issueID,
			Title:  "Fix deploy",
			Status: "in_progress",
		},
		labels: []db.IssueLabel{
			{Name: "area:backend"},
			{Name: "priority:p1"},
		},
		// no prs
	}, item)

	if _, present := got["issue_status_tag"]; present {
		t.Errorf("issue_status_tag should be absent when no label matches; got %q", got["issue_status_tag"])
	}
	if _, present := got["issue_pull_requests"]; present {
		t.Errorf("issue_pull_requests should be absent when issue has no PRs; got %q", got["issue_pull_requests"])
	}
}

func TestPushInboxSkipsNonIssueWhenNotifySessionEndpoint(t *testing.T) {
	socketPath := ccConnectTestSocketPath(t)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	seenCh := make(chan notifyRequest, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req notifyRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		seenCh <- req
		w.WriteHeader(http.StatusOK)
	})}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			t.Errorf("serve unix socket: %v", err)
		}
	}()
	defer srv.Close()

	cc := NewCCConnectClientWithConfig(CCConnectConfig{
		SocketPath:     socketPath,
		NotifyEndpoint: "notify-session",
	})
	PushInbox(context.Background(), nil, cc, fakeInboxMetadataLookup{
		user: db.User{Email: "1001@alibaba-inc.com"},
	}, db.InboxItem{
		RecipientID: makeTestUUID(0x11),
		Title:       "Non issue notification",
		Type:        "system_notice",
		Severity:    "info",
	})

	select {
	case req := <-seenCh:
		t.Fatalf("unexpected notify-session request for non-issue inbox: %#v", req)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestPushInboxSendsNonIssueToNotifyEndpoint(t *testing.T) {
	socketPath := ccConnectTestSocketPath(t)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	seenCh := make(chan notifyRequest, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/notify" {
			t.Errorf("path = %q; want /notify", r.URL.Path)
		}
		var req notifyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		seenCh <- req
		w.WriteHeader(http.StatusOK)
	})}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			t.Errorf("serve unix socket: %v", err)
		}
	}()
	defer srv.Close()

	cc := NewCCConnectClientWithConfig(CCConnectConfig{
		SocketPath:     socketPath,
		NotifyEndpoint: "notify",
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	PushInbox(ctx, nil, cc, fakeInboxMetadataLookup{
		user: db.User{Email: "1001@alibaba-inc.com"},
	}, db.InboxItem{
		RecipientID: makeTestUUID(0x11),
		Title:       "Non issue notification",
		Type:        "system_notice",
		Severity:    "info",
	})

	select {
	case req := <-seenCh:
		if req.UserID != "1001" || req.Title != "Non issue notification" || req.Metadata["issue_id"] != "" {
			t.Fatalf("request = %#v", req)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for /notify request")
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
