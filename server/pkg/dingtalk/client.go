// Package dingtalk talks to the DingTalk corporate-internal-app OpenAPI v1.0.
//
// We use it to mirror inbox notifications into a 1:1 chat with the recipient
// via a chatbot ("单聊机器人"). The recipient is identified by their DingTalk
// staffId, which for Alibaba employees equals the local-part of their
// `<工号>@alibaba-inc.com` email.
package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultAccessTokenURL = "https://api.dingtalk.com/v1.0/oauth2/accessToken"
	defaultBatchSendURL   = "https://api.dingtalk.com/v1.0/robot/oToMessages/batchSend"
	defaultGroupSendURL   = "https://api.dingtalk.com/v1.0/robot/groupMessages/send"
	defaultRequestTimeout = 5 * time.Second
	tokenRefreshSafety    = 60 * time.Second
)

// Config holds DingTalk corporate-internal-app credentials.
type Config struct {
	AppKey    string
	AppSecret string
	RobotCode string

	// Optional overrides used by tests.
	AccessTokenURL string
	BatchSendURL   string
	GroupSendURL   string
	HTTPClient     *http.Client
}

// Client is safe for concurrent use.
type Client struct {
	cfg Config
	hc  *http.Client

	mu          sync.Mutex
	cachedToken string
	tokenExpiry time.Time
}

// NewClient returns nil when required fields are missing — callers can
// keep the field nil and skip pushes without branching on every call site.
func NewClient(cfg Config) *Client {
	if cfg.AppKey == "" || cfg.AppSecret == "" || cfg.RobotCode == "" {
		return nil
	}
	if cfg.AccessTokenURL == "" {
		cfg.AccessTokenURL = defaultAccessTokenURL
	}
	if cfg.BatchSendURL == "" {
		cfg.BatchSendURL = defaultBatchSendURL
	}
	if cfg.GroupSendURL == "" {
		cfg.GroupSendURL = defaultGroupSendURL
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: defaultRequestTimeout}
	}
	return &Client{cfg: cfg, hc: hc}
}

// Enabled reports whether the receiver is non-nil and configured. Always
// false for a nil receiver, so call sites can write `if c.Enabled()` even
// when Dingtalk wiring is skipped.
func (c *Client) Enabled() bool { return c != nil }

// AccessToken returns a cached token, refreshing when within
// tokenRefreshSafety of expiry.
func (c *Client) AccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cachedToken != "" && time.Until(c.tokenExpiry) > tokenRefreshSafety {
		return c.cachedToken, nil
	}

	body, _ := json.Marshal(map[string]string{
		"appKey":    c.cfg.AppKey,
		"appSecret": c.cfg.AppSecret,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.AccessTokenURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("dingtalk: get accessToken: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("dingtalk: get accessToken: status %d body=%s", resp.StatusCode, string(raw))
	}
	var parsed struct {
		AccessToken string `json:"accessToken"`
		ExpireIn    int    `json:"expireIn"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("dingtalk: parse accessToken response: %w", err)
	}
	if parsed.AccessToken == "" {
		return "", errors.New("dingtalk: accessToken response missing token")
	}
	c.cachedToken = parsed.AccessToken
	if parsed.ExpireIn <= 0 {
		parsed.ExpireIn = 7200
	}
	c.tokenExpiry = time.Now().Add(time.Duration(parsed.ExpireIn) * time.Second)
	return c.cachedToken, nil
}

// BatchSendOTOMarkdown delivers a `sampleMarkdown` chatbot message to one or
// more DingTalk users. msgParam is required to be a JSON string by the
// upstream API, so we marshal `{title,text}` into a string here rather than
// nesting it as an object.
func (c *Client) BatchSendOTOMarkdown(ctx context.Context, userIDs []string, title, markdown string) error {
	if !c.Enabled() || len(userIDs) == 0 {
		return nil
	}
	token, err := c.AccessToken(ctx)
	if err != nil {
		return err
	}
	param, _ := json.Marshal(map[string]string{
		"title": title,
		"text":  markdown,
	})
	body, _ := json.Marshal(map[string]any{
		"robotCode": c.cfg.RobotCode,
		"userIds":   userIDs,
		"msgKey":    "sampleMarkdown",
		"msgParam":  string(param),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BatchSendURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("dingtalk: batchSendOTO: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("dingtalk: batchSendOTO: status %d body=%s", resp.StatusCode, string(raw))
	}
	return nil
}

// SendGroupMarkdown delivers a `sampleMarkdown` chatbot message to a DingTalk
// group by openConversationId.
func (c *Client) SendGroupMarkdown(ctx context.Context, openConversationID, markdown string) error {
	if !c.Enabled() || strings.TrimSpace(openConversationID) == "" {
		return nil
	}
	token, err := c.AccessToken(ctx)
	if err != nil {
		return err
	}
	param, _ := json.Marshal(map[string]string{
		"text": markdown,
	})
	body, _ := json.Marshal(map[string]any{
		"robotCode":          c.cfg.RobotCode,
		"openConversationId": openConversationID,
		"msgKey":             "sampleMarkdown",
		"msgParam":           string(param),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.GroupSendURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("dingtalk: groupMessages/send: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("dingtalk: groupMessages/send: status %d body=%s", resp.StatusCode, string(raw))
	}
	return nil
}

// UserIDFromAlibabaEmail extracts the DingTalk staffId from an
// `<工号>@alibaba-inc.com` email. The boolean reports whether the domain
// matches; non-matching emails return the local-part anyway so callers can
// log for debugging, but should not push.
func UserIDFromAlibabaEmail(email string) (string, bool) {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return "", false
	}
	local := strings.TrimSpace(email[:at])
	domain := strings.ToLower(strings.TrimSpace(email[at+1:]))
	if local == "" {
		return "", false
	}
	return local, domain == "alibaba-inc.com"
}
