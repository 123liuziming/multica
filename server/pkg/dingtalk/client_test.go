package dingtalk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestUserIDFromAlibabaEmail(t *testing.T) {
	cases := []struct {
		email  string
		wantID string
		wantOK bool
	}{
		{"123456@alibaba-inc.com", "123456", true},
		{"liuziming@alibaba-inc.com", "liuziming", true},
		{"foo@example.com", "foo", false},
		{"@alibaba-inc.com", "", false},
		{"plain", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		gotID, gotOK := UserIDFromAlibabaEmail(c.email)
		if gotID != c.wantID || gotOK != c.wantOK {
			t.Errorf("UserIDFromAlibabaEmail(%q) = (%q,%v); want (%q,%v)",
				c.email, gotID, gotOK, c.wantID, c.wantOK)
		}
	}
}

func TestNewClientNilWhenConfigMissing(t *testing.T) {
	cases := []Config{
		{},
		{AppKey: "k"},
		{AppKey: "k", AppSecret: "s"},
		{AppKey: "k", RobotCode: "r"},
	}
	for i, c := range cases {
		if NewClient(c) != nil {
			t.Errorf("case %d: expected nil client for incomplete config %+v", i, c)
		}
	}
}

func TestEnabledOnNilReceiver(t *testing.T) {
	var c *Client
	if c.Enabled() {
		t.Error("nil client should not be Enabled")
	}
}

func TestAccessTokenCaches(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"tok-1","expireIn":7200}`))
	}))
	defer srv.Close()

	c := NewClient(Config{
		AppKey: "k", AppSecret: "s", RobotCode: "r",
		AccessTokenURL: srv.URL,
	})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	for i := 0; i < 3; i++ {
		got, err := c.AccessToken(context.Background())
		if err != nil {
			t.Fatalf("AccessToken: %v", err)
		}
		if got != "tok-1" {
			t.Errorf("got %q; want tok-1", got)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 fetch (cached), got %d", calls.Load())
	}
}

func TestAccessTokenRefreshNearExpiry(t *testing.T) {
	tokens := []string{"tok-A", "tok-B"}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := int(calls.Add(1) - 1)
		if idx >= len(tokens) {
			idx = len(tokens) - 1
		}
		_, _ = w.Write([]byte(`{"accessToken":"` + tokens[idx] + `","expireIn":1}`))
	}))
	defer srv.Close()

	c := NewClient(Config{AppKey: "k", AppSecret: "s", RobotCode: "r", AccessTokenURL: srv.URL})
	got, err := c.AccessToken(context.Background())
	if err != nil || got != "tok-A" {
		t.Fatalf("first: got=%q err=%v", got, err)
	}
	got2, err := c.AccessToken(context.Background())
	if err != nil || got2 != "tok-B" {
		t.Fatalf("second: got=%q err=%v", got2, err)
	}
}

func TestAccessTokenErrorOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"InvalidAuthentication","message":"bad creds"}`))
	}))
	defer srv.Close()

	c := NewClient(Config{AppKey: "k", AppSecret: "s", RobotCode: "r", AccessTokenURL: srv.URL})
	if _, err := c.AccessToken(context.Background()); err == nil {
		t.Fatal("expected error on 403 response")
	}
}

func TestBatchSendOTOMarkdownEncodesRequest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"accessToken":"tok","expireIn":7200}`))
	})

	type seenT struct {
		Auth     string
		Method   string
		Body     map[string]any
		MsgParam map[string]string
	}
	seenCh := make(chan seenT, 1)
	mux.HandleFunc("/send", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		var mp map[string]string
		if s, ok := body["msgParam"].(string); ok {
			_ = json.Unmarshal([]byte(s), &mp)
		}
		seenCh <- seenT{
			Auth:     r.Header.Get("x-acs-dingtalk-access-token"),
			Method:   r.Method,
			Body:     body,
			MsgParam: mp,
		}
		_, _ = w.Write([]byte(`{"processQueryKey":"q1"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(Config{
		AppKey: "k", AppSecret: "s", RobotCode: "robot-x",
		AccessTokenURL: srv.URL + "/token",
		BatchSendURL:   srv.URL + "/send",
	})
	if c == nil {
		t.Fatal("nil client")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.BatchSendOTOMarkdown(ctx, []string{"u1", "u2"}, "Hello", "**body**"); err != nil {
		t.Fatalf("BatchSendOTOMarkdown: %v", err)
	}

	seen := <-seenCh
	if seen.Method != http.MethodPost {
		t.Errorf("method = %s; want POST", seen.Method)
	}
	if seen.Auth != "tok" {
		t.Errorf("token header = %q", seen.Auth)
	}
	if seen.Body["robotCode"] != "robot-x" {
		t.Errorf("robotCode = %v", seen.Body["robotCode"])
	}
	if seen.Body["msgKey"] != "sampleMarkdown" {
		t.Errorf("msgKey = %v", seen.Body["msgKey"])
	}
	users, _ := seen.Body["userIds"].([]any)
	if len(users) != 2 {
		t.Errorf("userIds len = %d", len(users))
	}
	if seen.MsgParam["title"] != "Hello" || seen.MsgParam["text"] != "**body**" {
		t.Errorf("msgParam = %#v", seen.MsgParam)
	}
}

func TestBatchSendOTOMarkdownNoOpOnEmptyUsers(t *testing.T) {
	c := NewClient(Config{AppKey: "k", AppSecret: "s", RobotCode: "r"})
	if err := c.BatchSendOTOMarkdown(context.Background(), nil, "t", "b"); err != nil {
		t.Errorf("expected nil error on empty users, got %v", err)
	}
}

func TestBatchSendOTOMarkdownNoOpOnNilClient(t *testing.T) {
	var c *Client
	if err := c.BatchSendOTOMarkdown(context.Background(), []string{"u"}, "t", "b"); err != nil {
		t.Errorf("nil client should be no-op, got %v", err)
	}
}
