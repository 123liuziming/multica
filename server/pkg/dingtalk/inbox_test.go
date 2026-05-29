package dingtalk

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/notifysummary"
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

type fakeSummaryDispatcher struct {
	mu    sync.Mutex
	calls []struct {
		StaffID  string
		IssueID  string
		Settings notifysummary.Settings
		Notif    notifysummary.QueuedNotification
	}
}

func (f *fakeSummaryDispatcher) Enqueue(staffID, issueID string, settings notifysummary.Settings, n notifysummary.QueuedNotification) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct {
		StaffID  string
		IssueID  string
		Settings notifysummary.Settings
		Notif    notifysummary.QueuedNotification
	}{staffID, issueID, settings, n})
}

func (f *fakeSummaryDispatcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func ccSocketCapturingNotify(t *testing.T) (string, <-chan notifyRequest, func()) {
	t.Helper()
	socketPath := ccConnectTestSocketPath(t)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	ch := make(chan notifyRequest, 4)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/notify" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		var req notifyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ch <- req
		w.WriteHeader(http.StatusOK)
	})}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			t.Errorf("serve unix socket: %v", err)
		}
	}()
	return socketPath, ch, func() { srv.Close() }
}

func TestPushInbox_DirectPathWhenSummaryDisabled(t *testing.T) {
	socketPath, ch, closeSrv := ccSocketCapturingNotify(t)
	defer closeSrv()

	cc := NewCCConnectClient(socketPath)
	dispatcher := &fakeSummaryDispatcher{}
	PushInbox(context.Background(), nil, cc, dispatcher, fakeInboxMetadataLookup{
		user:      db.User{Email: "1001@alibaba-inc.com"},
		workspace: db.Workspace{Settings: []byte(`{}`)}, // notify_summary absent → default disabled
		issue:     db.Issue{Number: 7},
	}, db.InboxItem{
		RecipientID: makeTestUUID(0x11),
		WorkspaceID: makeTestUUID(0xaa),
		IssueID:     makeTestUUID(0xbb),
		Title:       "x",
		Type:        "status_changed",
	})

	select {
	case req := <-ch:
		if req.NotifyUser == nil || *req.NotifyUser != true {
			t.Errorf("notify_user = %v; want true on disabled path", req.NotifyUser)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("expected /notify call on direct path")
	}
	if dispatcher.count() != 0 {
		t.Errorf("dispatcher.count = %d; want 0 on disabled path", dispatcher.count())
	}
}

func TestPushInbox_EnqueuesWhenSummaryEnabled(t *testing.T) {
	socketPath, ch, closeSrv := ccSocketCapturingNotify(t)
	defer closeSrv()

	cc := NewCCConnectClient(socketPath)
	dispatcher := &fakeSummaryDispatcher{}
	wsSettings := []byte(`{"notify_summary":{"enabled":true,"idle_wait_secs":5,"max_wait_secs":15,"summary_length":200}}`)
	PushInbox(context.Background(), nil, cc, dispatcher, fakeInboxMetadataLookup{
		user:      db.User{Email: "1001@alibaba-inc.com"},
		workspace: db.Workspace{Settings: wsSettings, IssuePrefix: "ACME", Slug: "acme"},
		issue:     db.Issue{Number: 7, Title: "test", Status: "in_progress"},
	}, db.InboxItem{
		RecipientID: makeTestUUID(0x11),
		WorkspaceID: makeTestUUID(0xaa),
		IssueID:     makeTestUUID(0xbb),
		Title:       "x",
		Type:        "status_changed",
	})

	select {
	case req := <-ch:
		if req.NotifyUser == nil || *req.NotifyUser != false {
			t.Errorf("notify_user = %v; want false on summary path", req.NotifyUser)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("expected /notify call (notify_user=false) on summary path")
	}
	// Dispatcher.Enqueue runs inside the PushInbox goroutine; wait with a
	// proper deadline so the test doesn't flake on loaded CI runners.
	waitForCount := func() bool { return dispatcher.count() > 0 }
	dl := time.Now().Add(2 * time.Second)
	for time.Now().Before(dl) && !waitForCount() {
		time.Sleep(5 * time.Millisecond)
	}
	if dispatcher.count() != 1 {
		t.Fatalf("dispatcher.count = %d; want 1", dispatcher.count())
	}
	// After summary enqueue succeeds, no second /notify call should arrive.
	select {
	case extra := <-ch:
		t.Fatalf("unexpected second /notify after summary enqueue: notify_user=%v", extra.NotifyUser)
	case <-time.After(300 * time.Millisecond):
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
