package dingtalk

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ccConnectTestSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ccconnect-")
	if err != nil {
		t.Fatalf("create socket temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "api.sock")
}

func TestCCConnectSendNotificationIncludesMetadata(t *testing.T) {
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

	c := NewCCConnectClient(socketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = c.SendNotification(ctx, "123456", "Issue updated", "markdown body", map[string]string{
		"workspace_id":      "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"issue_id":          "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		"issue_title":       "Fix deploy",
		"issue_status":      "in_progress",
		"issue_create_time": "2026-05-27T01:30:00Z",
		"inbox_type":        "status_changed",
	})
	if err != nil {
		t.Fatalf("SendNotification: %v", err)
	}

	select {
	case req := <-seenCh:
		if req.Platform != "dingtalk" || req.UserID != "123456" || req.Title != "Issue updated" || req.Content != "markdown body" {
			t.Fatalf("request = %#v", req)
		}
		if req.Metadata["workspace_id"] != "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" {
			t.Errorf("workspace_id metadata = %q", req.Metadata["workspace_id"])
		}
		if req.Metadata["issue_id"] != "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb" {
			t.Errorf("issue_id metadata = %q", req.Metadata["issue_id"])
		}
		if req.Metadata["inbox_type"] != "status_changed" {
			t.Errorf("inbox_type metadata = %q", req.Metadata["inbox_type"])
		}
		if req.Metadata["issue_title"] != "Fix deploy" || req.Metadata["issue_status"] != "in_progress" || req.Metadata["issue_create_time"] != "2026-05-27T01:30:00Z" {
			t.Errorf("issue metadata = %#v", req.Metadata)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for request")
	}
}

func TestCCConnectSendNotificationCanUseNotifySessionEndpoint(t *testing.T) {
	socketPath := ccConnectTestSocketPath(t)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	seenCh := make(chan notifyRequest, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/notify-session" {
			t.Errorf("path = %q; want /notify-session", r.URL.Path)
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

	c := NewCCConnectClientWithConfig(CCConnectConfig{
		SocketPath:     socketPath,
		NotifyEndpoint: "notify-session",
	})
	if !c.UsesNotifySessionEndpoint() {
		t.Fatal("client should report notify-session endpoint")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = c.SendNotification(ctx, "123456", "Issue updated", "markdown body", map[string]string{
		"workspace_id":      "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"issue_id":          "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		"issue_title":       "Fix deploy",
		"issue_status":      "in_progress",
		"issue_create_time": "2026-05-27T01:30:00Z",
		"inbox_type":        "status_changed",
	})
	if err != nil {
		t.Fatalf("SendNotification: %v", err)
	}

	select {
	case req := <-seenCh:
		if req.Platform != "dingtalk" || req.UserID != "123456@alibaba-inc.com" || req.Title != "Issue updated" || req.Content != "markdown body" {
			t.Fatalf("request = %#v", req)
		}
		if req.Metadata["issue_id"] != "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb" || req.Metadata["issue_title"] != "Fix deploy" || req.Metadata["issue_status"] != "in_progress" || req.Metadata["issue_create_time"] != "2026-05-27T01:30:00Z" {
			t.Errorf("metadata = %#v", req.Metadata)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for request")
	}
}

func TestEnsureAlibabaEmail(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"1001", "1001@alibaba-inc.com"},
		{"1001@alibaba-inc.com", "1001@alibaba-inc.com"},
		{" 1001 ", "1001@alibaba-inc.com"},
		{"someone@example.com", "someone@example.com"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := ensureAlibabaEmail(tt.in); got != tt.want {
			t.Fatalf("ensureAlibabaEmail(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeNotifyEndpoint(t *testing.T) {
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"", "/notify", true},
		{"notify", "/notify", true},
		{"/notify", "/notify", true},
		{"notify-session", "/notify-session", true},
		{"/notify-session", "/notify-session", true},
		{"session", "/notify-session", true},
		{"bad", "/notify", false},
	}
	for _, tt := range tests {
		got, ok := NormalizeNotifyEndpoint(tt.in)
		if got != tt.want || ok != tt.wantOK {
			t.Fatalf("NormalizeNotifyEndpoint(%q) = (%q, %v); want (%q, %v)", tt.in, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestCCConnectSendQuestionCardIncludesMetadata(t *testing.T) {
	socketPath := ccConnectTestSocketPath(t)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	seenCh := make(chan QuestionCardRequest, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cards/question" {
			t.Errorf("path = %q; want /cards/question", r.URL.Path)
		}
		var req QuestionCardRequest
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

	c := NewCCConnectClient(socketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = c.SendQuestionCard(ctx, QuestionCardRequest{
		UserID: "123456",
		CardData: QuestionCardData{
			WorkspaceID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			IssueID:     "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
			IssueTitle:  "Fix deploy",
			QuestionURL: "https://app.example.com/ws/questions?issueId=bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb&questionId=cccccccc-cccc-cccc-cccc-cccccccccccc",
			QuestionID:  "cccccccc-cccc-cccc-cccc-cccccccccccc",
			Question:    "Which deploy target should I use?",
			UserID:      "dddddddd-dddd-dddd-dddd-dddddddddddd",
			AgentID:     "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee",
			AgentName:   "Release agent",
			Options: []QuestionCardOption{
				{Index: 0, Value: "0", Label: "Staging"},
				{Index: 1, Value: "1", Label: "Production", Description: "Use with caution"},
			},
		},
		Metadata: map[string]string{
			"workspace_id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			"issue_id":     "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
			"issueTitle":   "Fix deploy",
			"questionUrl":  "https://app.example.com/ws/questions?issueId=bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb&questionId=cccccccc-cccc-cccc-cccc-cccccccccccc",
			"question_id":  "cccccccc-cccc-cccc-cccc-cccccccccccc",
			"user_id":      "dddddddd-dddd-dddd-dddd-dddddddddddd",
			"agent_id":     "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee",
			"agentName":    "Release agent",
		},
	})
	if err != nil {
		t.Fatalf("SendQuestionCard: %v", err)
	}

	select {
	case req := <-seenCh:
		if req.Platform != "dingtalk" || req.UserID != "123456" || req.SchemaID != QuestionCardSchemaID {
			t.Fatalf("request = %#v", req)
		}
		if req.CardData.QuestionID != "cccccccc-cccc-cccc-cccc-cccccccccccc" {
			t.Errorf("question_id = %q", req.CardData.QuestionID)
		}
		if req.CardData.IssueTitle != "Fix deploy" {
			t.Errorf("issueTitle = %q", req.CardData.IssueTitle)
		}
		if req.CardData.QuestionURL == "" {
			t.Error("questionUrl is empty")
		}
		if req.CardData.AgentName != "Release agent" {
			t.Errorf("agentName = %q", req.CardData.AgentName)
		}
		if len(req.CardData.Options) != 2 || req.CardData.Options[1].Value != "1" {
			t.Errorf("options = %#v", req.CardData.Options)
		}
		if req.Metadata["user_id"] != "dddddddd-dddd-dddd-dddd-dddddddddddd" {
			t.Errorf("user_id metadata = %q", req.Metadata["user_id"])
		}
		if req.Metadata["question_id"] != "cccccccc-cccc-cccc-cccc-cccccccccccc" {
			t.Errorf("question_id metadata = %q", req.Metadata["question_id"])
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for request")
	}
}

func TestCCConnectSendQuestionCardRequiresWorkspaceID(t *testing.T) {
	c := NewCCConnectClient(filepath.Join(t.TempDir(), "missing.sock"))
	err := c.SendQuestionCard(context.Background(), QuestionCardRequest{
		UserID: "123456",
		CardData: QuestionCardData{
			QuestionID: "cccccccc-cccc-cccc-cccc-cccccccccccc",
			Question:   "Which deploy target should I use?",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "card_data.workspace_id") {
		t.Fatalf("error = %v", err)
	}
}

func TestCCConnectSendSessionMessageIncludesMetadata(t *testing.T) {
	socketPath := ccConnectTestSocketPath(t)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	seenCh := make(chan sendRequest, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/send" {
			t.Errorf("path = %q; want /send", r.URL.Path)
		}
		var req sendRequest
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

	c := NewCCConnectClient(socketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = c.SendSessionMessage(ctx, "dingtalk:g:cid123", "markdown body", map[string]string{
		"workspace_id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"issue_id":     "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		"inbox_type":   "status_changed",
	})
	if err != nil {
		t.Fatalf("SendSessionMessage: %v", err)
	}

	select {
	case req := <-seenCh:
		if req.SessionKey != "dingtalk:g:cid123" || req.Message != "markdown body" {
			t.Fatalf("request = %#v", req)
		}
		if req.Metadata["workspace_id"] != "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" {
			t.Errorf("workspace_id metadata = %q", req.Metadata["workspace_id"])
		}
		if req.Metadata["issue_id"] != "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb" {
			t.Errorf("issue_id metadata = %q", req.Metadata["issue_id"])
		}
		if req.Metadata["inbox_type"] != "status_changed" {
			t.Errorf("inbox_type metadata = %q", req.Metadata["inbox_type"])
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for request")
	}
}
