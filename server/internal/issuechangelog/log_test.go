package issuechangelog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestRegisterWritesIssueEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "changes.json")
	t.Setenv(pathEnv, path)

	bus := events.New()
	Register(bus)

	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: "workspace-1",
		ActorType:   "member",
		ActorID:     "user-1",
		Payload: map[string]any{
			"issue": map[string]any{
				"id":    "issue-1",
				"title": "Test issue",
			},
		},
	})
	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: "workspace-1",
		ActorType:   "agent",
		ActorID:     "agent-1",
		Payload: map[string]any{
			"comment": map[string]any{
				"id":       "comment-1",
				"issue_id": "issue-1",
				"content":  "Done",
			},
		},
	})

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read change log: %v", err)
	}
	var data fileData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("decode change log: %v", err)
	}
	if data.Version != 1 {
		t.Fatalf("version = %d, want 1", data.Version)
	}
	if len(data.Events) != 2 {
		t.Fatalf("events length = %d, want 2", len(data.Events))
	}
	if data.Events[0].EventType != protocol.EventIssueCreated || data.Events[0].IssueID != "issue-1" {
		t.Fatalf("first event = %+v", data.Events[0])
	}
	if data.Events[1].EventType != protocol.EventCommentCreated || data.Events[1].IssueID != "issue-1" {
		t.Fatalf("second event = %+v", data.Events[1])
	}
}

func TestRegisterIgnoresNonIssueEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "changes.json")
	t.Setenv(pathEnv, path)

	bus := events.New()
	Register(bus)
	bus.Publish(events.Event{
		Type:        protocol.EventProjectCreated,
		WorkspaceID: "workspace-1",
		ActorType:   "member",
		ActorID:     "user-1",
		Payload:     map[string]any{"project_id": "project-1"},
	})

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stat change log error = %v, want not exist", err)
	}
}

func TestRegisterWritesEventsWithNestedIssueID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "changes.json")
	t.Setenv(pathEnv, path)

	bus := events.New()
	Register(bus)
	bus.Publish(events.Event{
		Type:        protocol.EventTaskCompleted,
		WorkspaceID: "workspace-1",
		ActorType:   "agent",
		ActorID:     "agent-1",
		Payload: map[string]any{
			"task": map[string]any{
				"id":       "task-1",
				"issue_id": "issue-1",
				"status":   "completed",
			},
		},
	})

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read change log: %v", err)
	}
	var data fileData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("decode change log: %v", err)
	}
	if len(data.Events) != 1 {
		t.Fatalf("events length = %d, want 1", len(data.Events))
	}
	if data.Events[0].EventType != protocol.EventTaskCompleted || data.Events[0].IssueID != "issue-1" {
		t.Fatalf("event = %+v", data.Events[0])
	}
}
