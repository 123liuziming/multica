package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
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
