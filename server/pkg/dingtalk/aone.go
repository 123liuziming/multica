package dingtalk

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var aoneTitleIDRe = regexp.MustCompile(`(?i)\[AONE-(\d+)\]`)

type aoneMirrorInfo struct {
	ID       string
	Assignee string
	Remarks  []string
}

var aoneMirrorDedup = struct {
	sync.Mutex
	seen map[string]time.Time
}{seen: make(map[string]time.Time)}

func pushAoneLinkedTargets(ctx context.Context, client *Client, ccClient *CCConnectClient, item db.InboxItem, skipUserID string) {
	if !client.Enabled() && !ccClient.Enabled() {
		return
	}

	aoneID := extractAoneID(item.Title)
	if aoneID == "" || !reserveAoneMirror(item, aoneID) {
		return
	}

	markdown := BuildInboxMarkdown(item)
	meta := inboxMetadata(item)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		info, err := fetchAoneMirrorInfo(ctx, aoneID)
		if err != nil {
			slog.Warn("dingtalk: fetch aone mirror targets failed", "aone_id", aoneID, "error", err)
			return
		}

		for _, userID := range resolveAoneAssigneeDingUserIDs(ctx, info.Assignee) {
			if userID == "" || userID == skipUserID {
				continue
			}
			sendExternalUserNotification(ctx, client, ccClient, userID, item.Title, markdown, meta, item.Type)
		}

		for _, groupID := range extractDingTalkGroupIDs(strings.Join(info.Remarks, "\n")) {
			sendExternalGroupNotification(ctx, client, ccClient, groupID, markdown, meta, item.Type)
		}
	}()
}

func extractAoneID(title string) string {
	m := aoneTitleIDRe.FindStringSubmatch(title)
	if len(m) != 2 {
		return ""
	}
	return m[1]
}

func reserveAoneMirror(item db.InboxItem, aoneID string) bool {
	sum := sha1.Sum([]byte(item.Title + "\x00" + item.Type + "\x00" + string(item.Body.String) + "\x00" + string(item.Details)))
	key := fmt.Sprintf("%s:%x", aoneID, sum)

	aoneMirrorDedup.Lock()
	defer aoneMirrorDedup.Unlock()
	if t, ok := aoneMirrorDedup.seen[key]; ok && time.Since(t) < 30*time.Second {
		return false
	}
	aoneMirrorDedup.seen[key] = time.Now()
	for k, t := range aoneMirrorDedup.seen {
		if time.Since(t) > time.Minute {
			delete(aoneMirrorDedup.seen, k)
		}
	}
	return true
}

func fetchAoneMirrorInfo(ctx context.Context, aoneID string) (aoneMirrorInfo, error) {
	cmd := exec.CommandContext(ctx, "a1", "project", "workitem", "get", aoneID, "--format", "json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return aoneMirrorInfo{}, fmt.Errorf("a1 workitem get: %w output=%s", err, strings.TrimSpace(string(out)))
	}
	return parseAoneMirrorInfo(out)
}

func parseAoneMirrorInfo(raw []byte) (aoneMirrorInfo, error) {
	var detail struct {
		ID     string `json:"id"`
		Fields []struct {
			Identifier string `json:"identifier"`
			Label      string `json:"label"`
			Value      string `json:"value"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(raw, &detail); err != nil {
		return aoneMirrorInfo{}, err
	}

	info := aoneMirrorInfo{ID: detail.ID}
	for _, f := range detail.Fields {
		identifier := strings.ToLower(strings.TrimSpace(f.Identifier))
		label := strings.TrimSpace(f.Label)
		switch {
		case identifier == "assignedto" || strings.Contains(label, "指派给"):
			info.Assignee = strings.TrimSpace(f.Value)
		case strings.Contains(label, "备注"):
			if v := strings.TrimSpace(f.Value); v != "" {
				info.Remarks = append(info.Remarks, v)
			}
		}
	}
	return info, nil
}

func resolveAoneAssigneeDingUserIDs(ctx context.Context, assignee string) []string {
	assignee = strings.TrimSpace(assignee)
	if assignee == "" || strings.HasPrefix(assignee, "WORKER_") {
		return nil
	}

	ids := map[string]bool{}
	for _, email := range emailRe.FindAllString(assignee, -1) {
		if id, ok := UserIDFromAlibabaEmail(email); ok {
			ids[id] = true
		}
	}
	if len(ids) > 0 {
		return sortedKeys(ids)
	}
	for _, id := range numericIDRe.FindAllString(assignee, -1) {
		ids[id] = true
	}
	if len(ids) > 0 {
		return sortedKeys(ids)
	}

	cmd := exec.CommandContext(ctx, "a1", "staff", "list", "--query", assignee, "--format", "json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Warn("dingtalk: resolve aone assignee failed", "assignee", assignee, "error", err, "output", strings.TrimSpace(string(out)))
		return nil
	}
	for _, id := range extractStaffDingUserIDs(out) {
		ids[id] = true
	}
	return sortedKeys(ids)
}

var (
	emailRe     = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@alibaba-inc\.com`)
	numericIDRe = regexp.MustCompile(`\b\d{4,}\b`)
)

func extractStaffDingUserIDs(raw []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var data any
	if err := dec.Decode(&data); err != nil {
		return nil
	}

	ids := map[string]bool{}
	for _, rec := range firstStaffRecords(data) {
		for _, v := range rec {
			s, ok := v.(string)
			if !ok {
				continue
			}
			if id, ok := UserIDFromAlibabaEmail(s); ok {
				ids[id] = true
			}
		}
		if len(ids) > 0 {
			return sortedKeys(ids)
		}
		for k, v := range rec {
			s, ok := v.(string)
			if !ok || s == "" {
				continue
			}
			lk := strings.ToLower(k)
			if strings.Contains(lk, "emp") || strings.Contains(lk, "staff") || strings.Contains(lk, "workno") || strings.Contains(lk, "userid") {
				if strings.HasPrefix(s, "WORKER_") {
					continue
				}
				ids[s] = true
			}
		}
		break
	}
	return sortedKeys(ids)
}

func firstStaffRecords(v any) []map[string]any {
	switch t := v.(type) {
	case []any:
		var rows []map[string]any
		for _, item := range t {
			if m, ok := item.(map[string]any); ok {
				rows = append(rows, m)
			}
		}
		return rows
	case map[string]any:
		for _, key := range []string{"data", "items", "result", "users", "list"} {
			if rows := firstStaffRecords(t[key]); len(rows) > 0 {
				return rows
			}
		}
		return []map[string]any{t}
	default:
		return nil
	}
}

var groupIDPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)dingtalk:g:([A-Za-z0-9._:-]+)`),
	regexp.MustCompile(`(?i)(?:openConversationId|conversationId)\s*[:=]\s*([A-Za-z0-9._:-]+)`),
	regexp.MustCompile(`(?i)(?:群聊|群)\s*[:=]\s*([A-Za-z0-9._:-]+)`),
	regexp.MustCompile(`\bcid[A-Za-z0-9._:-]*\b`),
}

func extractDingTalkGroupIDs(text string) []string {
	ids := map[string]bool{}
	for _, re := range groupIDPatterns {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			id := ""
			if len(m) > 1 {
				id = m[1]
			} else if len(m) == 1 {
				id = m[0]
			}
			id = strings.Trim(id, " \t\r\n,，;；)]}>'\"")
			if id != "" {
				ids[id] = true
			}
		}
	}
	return sortedKeys(ids)
}

func inboxMetadata(item db.InboxItem) map[string]string {
	meta := map[string]string{}
	if wsID := formatUUID(item.WorkspaceID); wsID != "" {
		meta["workspace_id"] = wsID
	}
	if issueID := formatUUID(item.IssueID); issueID != "" {
		meta["issue_id"] = issueID
	}
	if item.Type != "" {
		meta["inbox_type"] = item.Type
	}
	return meta
}

func sendExternalUserNotification(ctx context.Context, client *Client, ccClient *CCConnectClient, userID, title, markdown string, meta map[string]string, inboxType string) {
	if ccClient.Enabled() {
		if err := ccClient.SendNotification(ctx, userID, title, markdown, meta); err == nil {
			return
		} else {
			slog.Warn("dingtalk: push aone assignee via cc-connect failed, falling back to direct", "ding_user_id", userID, "inbox_type", inboxType, "error", err)
		}
	}
	if client.Enabled() {
		if err := client.BatchSendOTOMarkdown(ctx, []string{userID}, title, markdown); err != nil {
			slog.Warn("dingtalk: push aone assignee failed", "ding_user_id", userID, "inbox_type", inboxType, "error", err)
		}
	}
}

func sendExternalGroupNotification(ctx context.Context, client *Client, ccClient *CCConnectClient, groupID, markdown string, meta map[string]string, inboxType string) {
	if ccClient.Enabled() {
		if err := ccClient.SendSessionMessage(ctx, "dingtalk:g:"+groupID, markdown, meta); err == nil {
			return
		} else {
			slog.Warn("dingtalk: push aone group via cc-connect failed, falling back to direct", "group_id", groupID, "inbox_type", inboxType, "error", err)
		}
	}
	if client.Enabled() {
		if err := client.SendGroupMarkdown(ctx, groupID, markdown); err != nil {
			slog.Warn("dingtalk: push aone group failed", "group_id", groupID, "inbox_type", inboxType, "error", err)
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
