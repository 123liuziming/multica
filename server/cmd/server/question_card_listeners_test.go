package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dingtalk"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestQuestionCardListenerSendsIssueQuestionToCCConnect(t *testing.T) {
	socketDir, err := os.MkdirTemp("/tmp", "question-card-")
	if err != nil {
		t.Fatalf("create socket temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(socketDir) })
	socketPath := filepath.Join(socketDir, "api.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}

	seenCh := make(chan dingtalk.QuestionCardRequest, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cards/question" {
			t.Errorf("path = %q; want /cards/question", r.URL.Path)
		}
		var req dingtalk.QuestionCardRequest
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

	ctx := context.Background()
	t.Setenv("FRONTEND_ORIGIN", "https://app.example.com")
	queries := db.New(testPool)
	bus := events.New()
	registerQuestionCardListeners(bus, queries, dingtalk.NewCCConnectClient(socketPath))

	recipientEmail := "question-card-listener@alibaba-inc.com"
	recipientID := createTestUser(t, recipientEmail)
	t.Cleanup(func() { cleanupTestUser(t, recipientEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })
	addTestSubscriber(t, issueID, "member", recipientID, "test")

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id::text FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	bus.Publish(events.Event{
		Type:        protocol.EventQuestionCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "agent",
		ActorID:     agentID,
		Payload: map[string]any{
			"question": handler.QuestionResponse{
				ID:          "cccccccc-cccc-cccc-cccc-cccccccccccc",
				WorkspaceID: testWorkspaceID,
				TaskID:      "dddddddd-dddd-dddd-dddd-dddddddddddd",
				AgentID:     agentID,
				IssueID:     &issueID,
				Header:      "Need input",
				Question:    "Which branch should I target?",
				Options: []handler.QuestionOption{
					{Label: "main"},
					{Label: "release", Description: "Use the release branch"},
				},
				Status:    "pending",
				CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			},
		},
	})

	select {
	case req := <-seenCh:
		if req.Platform != "dingtalk" || req.UserID != "question-card-listener" || req.SchemaID != dingtalk.QuestionCardSchemaID {
			t.Fatalf("request = %#v", req)
		}
		if req.CardData.WorkspaceID != testWorkspaceID || req.CardData.IssueID != issueID {
			t.Fatalf("card_data workspace/issue = %#v", req.CardData)
		}
		if req.CardData.QuestionID != "cccccccc-cccc-cccc-cccc-cccccccccccc" {
			t.Errorf("question_id = %q", req.CardData.QuestionID)
		}
		if req.CardData.IssueTitle != "subscriber test issue" || req.Metadata["issueTitle"] != "subscriber test issue" {
			t.Errorf("issue title missing: card=%q metadata=%q", req.CardData.IssueTitle, req.Metadata["issueTitle"])
		}
		if req.CardData.QuestionURL != "https://app.example.com/integration-tests/questions?issueId="+issueID+"&questionId=cccccccc-cccc-cccc-cccc-cccccccccccc" {
			t.Errorf("questionUrl = %q", req.CardData.QuestionURL)
		}
		if req.CardData.AgentName != "Integration Test Agent" || req.Metadata["agentName"] != "Integration Test Agent" {
			t.Errorf("agent name missing: card=%q metadata=%q", req.CardData.AgentName, req.Metadata["agentName"])
		}
		if req.CardData.UserID != recipientID || req.Metadata["user_id"] != recipientID {
			t.Errorf("recipient user id missing: card=%q metadata=%q", req.CardData.UserID, req.Metadata["user_id"])
		}
		if len(req.CardData.Options) != 2 || req.CardData.Options[1].Value != "1" {
			t.Errorf("options = %#v", req.CardData.Options)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for question card request")
	}
}

func TestQuestionCardListenerGroupSessionOverrideSendsWithoutSubscribers(t *testing.T) {
	socketDir, err := os.MkdirTemp("/tmp", "question-card-group-")
	if err != nil {
		t.Fatalf("create socket temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(socketDir) })
	socketPath := filepath.Join(socketDir, "api.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}

	seenCh := make(chan dingtalk.QuestionCardRequest, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req dingtalk.QuestionCardRequest
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

	ctx := context.Background()
	t.Setenv("FRONTEND_ORIGIN", "https://app.example.com")
	t.Setenv(questionCardSessionKeyEnv, "dingtalk:g:conv123")
	queries := db.New(testPool)
	bus := events.New()
	registerQuestionCardListeners(bus, queries, dingtalk.NewCCConnectClient(socketPath))

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id::text FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	bus.Publish(events.Event{
		Type:        protocol.EventQuestionCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "agent",
		ActorID:     agentID,
		Payload: map[string]any{
			"question": handler.QuestionResponse{
				ID:          "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee",
				WorkspaceID: testWorkspaceID,
				TaskID:      "ffffffff-ffff-ffff-ffff-ffffffffffff",
				AgentID:     agentID,
				IssueID:     &issueID,
				Question:    "Which branch should I target?",
				Options:     []handler.QuestionOption{{Label: "main"}},
				Status:      "pending",
			},
		},
	})

	select {
	case req := <-seenCh:
		if req.UserID != "" || req.SessionKey != "dingtalk:g:conv123" {
			t.Fatalf("target = user_id %q session_key %q", req.UserID, req.SessionKey)
		}
		if req.CardData.SessionKey != "dingtalk:g:conv123" || req.Metadata["session_key"] != "dingtalk:g:conv123" {
			t.Fatalf("session key missing: card=%q metadata=%q", req.CardData.SessionKey, req.Metadata["session_key"])
		}
		if req.CardData.QuestionID != "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee" {
			t.Errorf("question_id = %q", req.CardData.QuestionID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for question card request")
	}
}

func TestBuildQuestionCardRequestUsesIssueWorkspaceWhenQuestionWorkspaceMissing(t *testing.T) {
	issueID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	question := handler.QuestionResponse{
		ID:       "cccccccc-cccc-cccc-cccc-cccccccccccc",
		TaskID:   "dddddddd-dddd-dddd-dddd-dddddddddddd",
		AgentID:  "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee",
		IssueID:  &issueID,
		Question: "Which branch should I target?",
		Options:  []handler.QuestionOption{{Label: "main"}},
		Status:   "pending",
	}
	issue := db.Issue{
		WorkspaceID: parseUUID(testWorkspaceID),
		Title:       "fallback workspace issue",
	}

	req := buildQuestionCardRequest(question, issue, "", "", "Agent", "user-1", "ding-1", "")

	if req.CardData.WorkspaceID != testWorkspaceID {
		t.Fatalf("card workspace_id = %q, want %q", req.CardData.WorkspaceID, testWorkspaceID)
	}
	if req.Metadata["workspace_id"] != testWorkspaceID {
		t.Fatalf("metadata workspace_id = %q, want %q", req.Metadata["workspace_id"], testWorkspaceID)
	}
}
