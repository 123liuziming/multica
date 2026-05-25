package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// CCConnectClient sends notifications through cc-connect's /notify API
// via a Unix socket. When configured, PushInbox uses this instead of
// calling the DingTalk API directly, so cc-connect can store context
// for reply detection.
type CCConnectClient struct {
	hc         *http.Client
	socketPath string
}

// NewCCConnectClient returns nil when socketPath is empty, matching the
// nil-is-disabled convention used by Client.
func NewCCConnectClient(socketPath string) *CCConnectClient {
	if socketPath == "" {
		return nil
	}
	return &CCConnectClient{
		socketPath: socketPath,
		hc: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", socketPath)
				},
			},
		},
	}
}

func (c *CCConnectClient) Enabled() bool { return c != nil }

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

// SendNotification sends a notification through cc-connect's /notify endpoint.
func (c *CCConnectClient) SendNotification(ctx context.Context, userID, title, content string, metadata map[string]string) error {
	if !c.Enabled() {
		return fmt.Errorf("ccconnect: client not configured")
	}

	body, err := json.Marshal(notifyRequest{
		Platform: "dingtalk",
		UserID:   userID,
		Title:    title,
		Content:  content,
		Metadata: metadata,
	})
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
