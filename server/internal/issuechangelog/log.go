package issuechangelog

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	defaultPath = "~/.multica_issue.json"
	pathEnv     = "MULTICA_ISSUE_CHANGE_LOG_PATH"
)

var writeMu sync.Mutex

type fileData struct {
	Version   int     `json:"version"`
	UpdatedAt string  `json:"updated_at"`
	Events    []Entry `json:"events"`
}

type Entry struct {
	ID          string `json:"id"`
	EventType   string `json:"event_type"`
	WorkspaceID string `json:"workspace_id"`
	ActorType   string `json:"actor_type"`
	ActorID     string `json:"actor_id"`
	IssueID     string `json:"issue_id,omitempty"`
	RecordedAt  string `json:"recorded_at"`
	Payload     any    `json:"payload"`
}

func Register(bus *events.Bus) {
	if bus == nil {
		return
	}
	bus.SubscribeAll(func(e events.Event) {
		if !isIssueChangeEvent(e) {
			return
		}
		if err := Append(e); err != nil {
			slog.Warn("write issue change log failed", "event_type", e.Type, "error", err)
		}
	})
}

func Append(e events.Event) error {
	path, err := expandPath(configuredPath())
	if err != nil {
		return err
	}

	entry := Entry{
		ID:          eventID(e),
		EventType:   e.Type,
		WorkspaceID: e.WorkspaceID,
		ActorType:   e.ActorType,
		ActorID:     e.ActorID,
		IssueID:     extractIssueID(e.Payload),
		RecordedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		Payload:     e.Payload,
	}

	writeMu.Lock()
	defer writeMu.Unlock()

	data, err := read(path)
	if err != nil {
		return err
	}
	data.Version = 1
	data.UpdatedAt = entry.RecordedAt
	data.Events = append(data.Events, entry)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create issue change log directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".multica-issue-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create issue change log temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		tmp.Close()
		return fmt.Errorf("encode issue change log: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close issue change log temp file: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("chmod issue change log temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename issue change log: %w", err)
	}
	return nil
}

func configuredPath() string {
	if v := strings.TrimSpace(os.Getenv(pathEnv)); v != "" {
		return v
	}
	return defaultPath
}

func expandPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("issue change log path is empty")
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

func read(path string) (fileData, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileData{Version: 1, Events: []Entry{}}, nil
		}
		return fileData{}, fmt.Errorf("read issue change log: %w", err)
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return fileData{Version: 1, Events: []Entry{}}, nil
	}

	var data fileData
	if err := json.Unmarshal(b, &data); err != nil {
		return fileData{}, fmt.Errorf("decode issue change log: %w", err)
	}
	if data.Events == nil {
		data.Events = []Entry{}
	}
	return data, nil
}

func eventID(e events.Event) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	base := e.WorkspaceID + "|" + e.ActorType + "|" + e.ActorID + "|" + e.Type + "|" + time.Now().UTC().Format(time.RFC3339Nano)
	return fmt.Sprintf("%x", base)
}

func extractIssueID(payload any) string {
	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return ""
	}
	return extractIssueIDValue(m)
}

func extractIssueIDValue(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	if v, ok := stringValue(m["issue_id"]); ok {
		return v
	}
	if issue, ok := m["issue"].(map[string]any); ok {
		if v, ok := stringValue(issue["id"]); ok {
			return v
		}
	}
	if comment, ok := m["comment"].(map[string]any); ok {
		if v, ok := stringValue(comment["issue_id"]); ok {
			return v
		}
	}
	for _, nested := range m {
		if v := extractIssueIDValue(nested); v != "" {
			return v
		}
	}
	return ""
}

func stringValue(v any) (string, bool) {
	s, ok := v.(string)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}

var issueEventTypes = map[string]struct{}{
	protocol.EventIssueCreated:         {},
	protocol.EventIssueUpdated:         {},
	protocol.EventIssueDeleted:         {},
	protocol.EventCommentCreated:       {},
	protocol.EventCommentUpdated:       {},
	protocol.EventCommentDeleted:       {},
	protocol.EventCommentResolved:      {},
	protocol.EventCommentUnresolved:    {},
	protocol.EventIssueReactionAdded:   {},
	protocol.EventIssueReactionRemoved: {},
	protocol.EventSubscriberAdded:      {},
	protocol.EventSubscriberRemoved:    {},
	protocol.EventIssueLabelsChanged:   {},
}

func isIssueChangeEvent(e events.Event) bool {
	if _, ok := issueEventTypes[e.Type]; ok {
		return true
	}
	return extractIssueID(e.Payload) != ""
}
