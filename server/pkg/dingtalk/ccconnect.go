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

// CCConnectClient talks to cc-connect over its Unix socket. It exposes three
// hot-path methods used by the inbox push:
//
//   - SendNotification — POST /notify, with notify_user gating the platform
//     delivery (false = Aone-mirror only, used by the summary path).
//   - SendSessionPrompt — POST /notify-session with a pre-rendered prompt,
//     injected into the user's active 1:1 session. Used by the notifysummary
//     dispatcher after a bucket flush.
//   - SendSessionMessage — POST /send for group-chat fan-out.
//   - SendQuestionCard — POST /cards/question for interactive cards.
type CCConnectClient struct {
	hc         *http.Client
	socketPath string
}

type CCConnectConfig struct {
	SocketPath string
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
	return &CCConnectClient{
		socketPath: cfg.SocketPath,
		hc: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", cfg.SocketPath)
				},
			},
		},
	}
}

func (c *CCConnectClient) Enabled() bool { return c != nil }

// ensureAlibabaEmail returns the input untouched if it already contains "@",
// otherwise appends "@alibaba-inc.com". /notify-session only accepts the
// full Alibaba email form.
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

type notifyRequest struct {
	Platform   string            `json:"platform"`
	UserID     string            `json:"user_id"`
	Title      string            `json:"title"`
	Content    string            `json:"content"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	NotifyUser *bool             `json:"notify_user,omitempty"`
}

type notifySessionRequest struct {
	Platform string `json:"platform,omitempty"`
	UserID   string `json:"user_id"`
	Prompt   string `json:"prompt"`
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

// SendNotification POSTs to cc-connect's /notify. userID is the bare DingTalk
// staff ID. notifyUser=true delivers the platform notification; false skips
// the platform send and only triggers the Aone-comment mirror (used by the
// summary path so the user only sees the rendered card later).
func (c *CCConnectClient) SendNotification(ctx context.Context, userID, title, content string, metadata map[string]string, notifyUser bool) error {
	if !c.Enabled() {
		return fmt.Errorf("ccconnect: client not configured")
	}

	nr := notifyRequest{
		Platform:   "dingtalk",
		UserID:     userID,
		Title:      title,
		Content:    content,
		Metadata:   metadata,
		NotifyUser: &notifyUser,
	}
	logAttrs := []any{
		"user_id", nr.UserID,
		"title", nr.Title,
		"content", nr.Content,
		"metadata", nr.Metadata,
		"notify_user", notifyUser,
	}
	if nr.Metadata != nil {
		if v := nr.Metadata["issue_id"]; v != "" {
			logAttrs = append(logAttrs, "issue_id", v)
		}
		if v := nr.Metadata["issue_title"]; v != "" {
			logAttrs = append(logAttrs, "issue_title", v)
		}
		if v := nr.Metadata["issue_identifier"]; v != "" {
			logAttrs = append(logAttrs, "issue_identifier", v)
		}
	}
	slog.InfoContext(ctx, "ccconnect: POST /notify", logAttrs...)

	body, err := json.Marshal(nr)
	if err != nil {
		return fmt.Errorf("ccconnect: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost/notify", bytes.NewReader(body))
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

// SendSessionPrompt POSTs a pre-rendered prompt to cc-connect's
// /notify-session. cc-connect treats it as if the user typed it into their
// 1:1 chat. userID may be bare (we expand to <id>@alibaba-inc.com on the
// wire because /notify-session rejects bare IDs).
//
// Implements notifysummary.SessionPromptSender.
func (c *CCConnectClient) SendSessionPrompt(ctx context.Context, userID, prompt string) error {
	if !c.Enabled() {
		return fmt.Errorf("ccconnect: client not configured")
	}
	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("ccconnect: prompt is required")
	}

	nsr := notifySessionRequest{
		Platform: "dingtalk",
		UserID:   ensureAlibabaEmail(userID),
		Prompt:   prompt,
	}
	slog.InfoContext(ctx, "ccconnect: POST /notify-session",
		"user_id", nsr.UserID,
		"prompt", nsr.Prompt,
	)

	body, err := json.Marshal(nsr)
	if err != nil {
		return fmt.Errorf("ccconnect: marshal session-prompt request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost/notify-session", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ccconnect: create session-prompt request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("ccconnect: session-prompt request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ccconnect: notify-session failed: status %d body=%s", resp.StatusCode, string(raw))
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

	slog.InfoContext(ctx, "ccconnect: POST /cards/question",
		"user_id", reqBody.UserID,
		"session_key", reqBody.SessionKey,
		"workspace_id", reqBody.CardData.WorkspaceID,
		"issue_id", reqBody.CardData.IssueID,
		"issue_title", reqBody.CardData.IssueTitle,
		"issue_identifier", reqBody.CardData.IssueIdentifier,
		"question_id", reqBody.CardData.QuestionID,
		"question", reqBody.CardData.Question,
		"agent_id", reqBody.CardData.AgentID,
		"agent_name", reqBody.CardData.AgentName,
		"multi_select", reqBody.CardData.MultiSelect,
		"options_count", len(reqBody.CardData.Options),
	)

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

	sr := sendRequest{
		SessionKey: sessionKey,
		Message:    message,
		Metadata:   metadata,
	}
	logAttrs := []any{
		"session_key", sr.SessionKey,
		"message", sr.Message,
		"metadata", sr.Metadata,
	}
	if sr.Metadata != nil {
		if v := sr.Metadata["issue_id"]; v != "" {
			logAttrs = append(logAttrs, "issue_id", v)
		}
		if v := sr.Metadata["issue_title"]; v != "" {
			logAttrs = append(logAttrs, "issue_title", v)
		}
		if v := sr.Metadata["issue_identifier"]; v != "" {
			logAttrs = append(logAttrs, "issue_identifier", v)
		}
	}
	slog.InfoContext(ctx, "ccconnect: POST /send", logAttrs...)

	body, err := json.Marshal(sr)
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
