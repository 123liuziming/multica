package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// CCConnectClient sends notifications through cc-connect's configured notify API
// via a Unix socket. When configured, PushInbox uses this instead of
// calling the DingTalk API directly, so cc-connect can store context
// for reply detection.
type CCConnectClient struct {
	hc                 *http.Client
	socketPath         string
	notifyEndpointPath string
}

type CCConnectConfig struct {
	SocketPath     string
	NotifyEndpoint string
}

// NewCCConnectClient returns nil when socketPath is empty, matching the
// nil-is-disabled convention used by Client.
func NewCCConnectClient(socketPath string) *CCConnectClient {
	return NewCCConnectClientWithConfig(CCConnectConfig{SocketPath: socketPath})
}

func NewCCConnectClientWithConfig(cfg CCConnectConfig) *CCConnectClient {
	if cfg.SocketPath == "" {
		return nil
	}
	endpointPath, ok := NormalizeNotifyEndpoint(cfg.NotifyEndpoint)
	if !ok {
		slog.Warn("ccconnect: unknown notify endpoint, defaulting to /notify",
			"value", cfg.NotifyEndpoint)
	}
	return &CCConnectClient{
		socketPath:         cfg.SocketPath,
		notifyEndpointPath: endpointPath,
		hc: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", cfg.SocketPath)
				},
			},
		},
	}
}

func (c *CCConnectClient) Enabled() bool { return c != nil }

func (c *CCConnectClient) NotifyEndpointPath() string {
	if c == nil {
		return ""
	}
	return c.notifyEndpointPath
}

func (c *CCConnectClient) UsesNotifySessionEndpoint() bool {
	return c != nil && c.notifyEndpointPath == "/notify-session"
}

// ensureAlibabaEmail returns the input untouched if it already contains "@",
// otherwise appends "@alibaba-inc.com". The /notify-session contract on
// cc-connect only accepts the full Alibaba email form; bare staff IDs are
// rejected on that side.
func ensureAlibabaEmail(userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return userID
	}
	if strings.Contains(userID, "@") {
		return userID
	}
	return userID + "@alibaba-inc.com"
}

// NormalizeNotifyEndpoint maps a user-supplied endpoint string to the path
// cc-connect actually serves. The bool is false for unrecognized values
// (defaulted to /notify) so the caller can surface a configuration warning.
func NormalizeNotifyEndpoint(endpoint string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(endpoint)) {
	case "", "notify", "/notify":
		return "/notify", true
	case "notify-session", "/notify-session", "session":
		return "/notify-session", true
	default:
		return "/notify", false
	}
}

type notifyRequest struct {
	Platform string            `json:"platform"`
	UserID   string            `json:"user_id"`
	Title    string            `json:"title"`
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type sendRequest struct {
	Project    string            `json:"project,omitempty"`
	SessionKey string            `json:"session_key"`
	Message    string            `json:"message"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

const QuestionCardSchemaID = "c3c3ede4-fed1-45fd-93b9-69dbd04239bb.schema"

// QuestionCardRequest is the cc-connect /cards/question contract.
//
// Top-level UserID is the DingTalk recipient user id, matching /notify.
// CardData.UserID and metadata["user_id"] carry the Multica user UUID.
// CardData.WorkspaceID and CardData.QuestionID are the authoritative
// identifiers cc-connect must pass to `multica question answer`.
type QuestionCardRequest struct {
	Platform   string            `json:"platform"`
	UserID     string            `json:"user_id,omitempty"`
	SessionKey string            `json:"session_key,omitempty"`
	SchemaID   string            `json:"schema_id"`
	CardData   QuestionCardData  `json:"card_data"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type QuestionCardData struct {
	WorkspaceID     string               `json:"workspace_id"`
	IssueID         string               `json:"issue_id"`
	IssueTitle      string               `json:"issueTitle,omitempty"`
	IssueIdentifier string               `json:"issue_identifier,omitempty"`
	QuestionURL     string               `json:"questionUrl,omitempty"`
	QuestionID      string               `json:"question_id"`
	Question        string               `json:"question"`
	UserID          string               `json:"user_id,omitempty"`
	SessionKey      string               `json:"session_key,omitempty"`
	TaskID          string               `json:"task_id,omitempty"`
	AgentID         string               `json:"agent_id"`
	AgentName       string               `json:"agentName,omitempty"`
	AgentNameText   string               `json:"agent_name,omitempty"`
	Header          string               `json:"header,omitempty"`
	Options         []QuestionCardOption `json:"options,omitempty"`
	MultiSelect     bool                 `json:"multi_select"`
	CreatedAt       string               `json:"created_at,omitempty"`
}

type QuestionCardOption struct {
	Index       int    `json:"index"`
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// SendNotification sends a notification through cc-connect's configured notify endpoint.
// Callers pass the bare DingTalk staff ID (e.g. "1001"); when targeting
// /notify-session the client expands it to "<id>@alibaba-inc.com" on the wire
// because that endpoint only accepts the full Alibaba email form.
func (c *CCConnectClient) SendNotification(ctx context.Context, userID, title, content string, metadata map[string]string) error {
	if !c.Enabled() {
		return fmt.Errorf("ccconnect: client not configured")
	}

	wireUserID := userID
	if c.UsesNotifySessionEndpoint() {
		wireUserID = ensureAlibabaEmail(userID)
	}
	body, err := json.Marshal(notifyRequest{
		Platform: "dingtalk",
		UserID:   wireUserID,
		Title:    title,
		Content:  content,
		Metadata: metadata,
	})
	if err != nil {
		return fmt.Errorf("ccconnect: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost"+c.notifyEndpointPath, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ccconnect: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("ccconnect: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ccconnect: notify failed: status %d body=%s", resp.StatusCode, string(raw))
	}
	return nil
}

// SendQuestionCard asks cc-connect to send a DingTalk interactive question card
// and register the callback context needed to answer it via the Multica CLI.
func (c *CCConnectClient) SendQuestionCard(ctx context.Context, reqBody QuestionCardRequest) error {
	if !c.Enabled() {
		return fmt.Errorf("ccconnect: client not configured")
	}
	if reqBody.Platform == "" {
		reqBody.Platform = "dingtalk"
	}
	if reqBody.SchemaID == "" {
		reqBody.SchemaID = QuestionCardSchemaID
	}
	reqBody.CardData.WorkspaceID = strings.TrimSpace(reqBody.CardData.WorkspaceID)
	reqBody.CardData.QuestionID = strings.TrimSpace(reqBody.CardData.QuestionID)
	reqBody.CardData.Question = strings.TrimSpace(reqBody.CardData.Question)
	if reqBody.CardData.WorkspaceID == "" {
		return fmt.Errorf("ccconnect: question card requires card_data.workspace_id")
	}
	if reqBody.CardData.QuestionID == "" {
		return fmt.Errorf("ccconnect: question card requires card_data.question_id")
	}
	if reqBody.CardData.Question == "" {
		return fmt.Errorf("ccconnect: question card requires card_data.question")
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("ccconnect: marshal question card request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost/cards/question", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ccconnect: create question card request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("ccconnect: question card request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ccconnect: question card failed: status %d body=%s", resp.StatusCode, string(raw))
	}
	return nil
}

// SendSessionMessage sends a proactive message through cc-connect's /send API.
// It is used for group chats because /notify is user-directed.
func (c *CCConnectClient) SendSessionMessage(ctx context.Context, sessionKey, message string, metadata map[string]string) error {
	if !c.Enabled() {
		return fmt.Errorf("ccconnect: client not configured")
	}

	body, err := json.Marshal(sendRequest{
		SessionKey: sessionKey,
		Message:    message,
		Metadata:   metadata,
	})
	if err != nil {
		return fmt.Errorf("ccconnect: marshal send request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost/send", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ccconnect: create send request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("ccconnect: send request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ccconnect: send failed: status %d body=%s", resp.StatusCode, string(raw))
	}
	return nil
}
