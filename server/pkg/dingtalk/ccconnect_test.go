package dingtalk

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestCCConnectSendNotificationIncludesMetadata(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "api.sock")
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
		"workspace_id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"issue_id":     "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		"inbox_type":   "status_changed",
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
	case <-ctx.Done():
		t.Fatal("timed out waiting for request")
	}
}

func TestCCConnectSendSessionMessageIncludesMetadata(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "api.sock")
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
