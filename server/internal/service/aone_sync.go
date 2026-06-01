package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var aoneNamespace = uuid.MustParse("a10e0000-0000-0000-0000-000000000000")

type AoneSyncService struct {
	Queries   *db.Queries
	TxStarter TxStarter
}

func NewAoneSyncService(q *db.Queries, tx TxStarter) *AoneSyncService {
	return &AoneSyncService{Queries: q, TxStarter: tx}
}

type AoneSyncResult struct {
	WorkspaceID    string   `json:"workspace_id"`
	AoneProjectID  string   `json:"aone_project_id"`
	TotalFetched   int      `json:"total_fetched"`
	Created        int      `json:"created"`
	Skipped        int      `json:"skipped_by_timestamp"`
	AlreadySynced  int      `json:"already_synced"`
	Failed         int      `json:"failed"`
	FailedDetails  []string `json:"failed_details,omitempty"`
	Error          string   `json:"error,omitempty"`
}

type aoneWorkItem struct {
	ID        json.Number `json:"identifier"`
	Title     string      `json:"subject"`
	Body      string      `json:"body"`
	Status    string      `json:"status"`
	Priority  string      `json:"priority"`
	Category   string      `json:"categoryIdentifier"`
	GmtCreate  string      `json:"gmtCreate"`
}

func (s *AoneSyncService) SyncAll(ctx context.Context) {
	workspaces, err := s.Queries.ListWorkspacesWithAoneSetting(ctx)
	if err != nil {
		slog.Warn("aone sync: failed to list workspaces", "error", err)
		return
	}
	if len(workspaces) == 0 {
		return
	}

	for _, ws := range workspaces {
		if err := ctx.Err(); err != nil {
			return
		}
		s.SyncWorkspace(ctx, ws)
	}
}

func (s *AoneSyncService) SyncWorkspace(ctx context.Context, ws db.Workspace) AoneSyncResult {
	wsID := util.UUIDToString(ws.ID)
	result := AoneSyncResult{WorkspaceID: wsID}

	projectID := extractAoneProjectID(ws.Settings)
	if projectID == "" {
		result.Error = "no aone_project_id configured"
		slog.Warn("aone sync: no aone_project_id", "workspace_id", wsID)
		return result
	}
	result.AoneProjectID = projectID

	minCreatedTs := extractAoneSyncMinCreatedTs(ws.Settings)

	members, err := s.Queries.ListMembers(ctx, ws.ID)
	if err != nil || len(members) == 0 {
		result.Error = "no members found in workspace"
		slog.Warn("aone sync: no members found", "workspace_id", wsID)
		return result
	}
	var creatorID pgtype.UUID
	for _, m := range members {
		if m.Role == "owner" {
			creatorID = m.UserID
			break
		}
	}
	if !creatorID.Valid {
		creatorID = members[0].UserID
	}

	slog.Info("aone sync: starting", "workspace_id", wsID, "aone_project_id", projectID, "min_created_ts", minCreatedTs)

	membersWithUser, err := s.Queries.ListMembersWithUser(ctx, ws.ID)
	if err != nil {
		slog.Warn("aone sync: failed to load members with user", "workspace_id", wsID, "error", err)
	}
	nicknameToUserID := make(map[string]pgtype.UUID)
	for _, m := range membersWithUser {
		if m.UserNickname.Valid && m.UserNickname.String != "" {
			nicknameToUserID[m.UserNickname.String] = m.UserID
		}
	}

	items, err := fetchAoneWorkItems(ctx, projectID)
	if err != nil {
		result.Error = fmt.Sprintf("failed to fetch aone work items: %v", err)
		slog.Warn("aone sync: fetch failed", "workspace_id", wsID, "aone_project_id", projectID, "error", err)
		return result
	}
	result.TotalFetched = len(items)
	slog.Info("aone sync: fetched work items", "workspace_id", wsID, "count", len(items))

	for _, item := range items {
		if err := ctx.Err(); err != nil {
			result.Error = "context cancelled"
			return result
		}

		aoneID := item.ID.String()

		if minCreatedTs > 0 && !isAfterTimestamp(item.GmtCreate, minCreatedTs) {
			result.Skipped++
			slog.Debug("aone sync: skipped by timestamp", "workspace_id", wsID, "aone_id", aoneID, "gmtCreate", item.GmtCreate)
			continue
		}

		if !strings.Contains(item.Title, "[HARNESS-AUTO]") {
			result.Skipped++
			continue
		}

		dedupUUID := deriveDedupUUID(ws.ID, aoneID)

		_, err := s.Queries.GetIssueByOrigin(ctx, db.GetIssueByOriginParams{
			WorkspaceID: ws.ID,
			OriginType:  pgtype.Text{String: "aone", Valid: true},
			OriginID:    dedupUUID,
		})
		if err == nil {
			result.AlreadySynced++
			slog.Debug("aone sync: already exists", "workspace_id", wsID, "aone_id", aoneID)
			continue
		}

		var aoneAssignee string
		if detail, err := fetchAoneWorkItemDetail(ctx, aoneID); err != nil {
			slog.Warn("aone sync: failed to fetch detail, using fallback", "workspace_id", wsID, "aone_id", aoneID, "error", err)
		} else {
			if detail.Description != "" {
				item.Body = detail.Description
			}
			aoneAssignee = detail.Assignee
		}

		issueCreatorID := creatorID
		if aoneAssignee != "" {
			if uid, ok := nicknameToUserID[aoneAssignee]; ok {
				issueCreatorID = uid
				slog.Debug("aone sync: matched creator by nickname", "workspace_id", wsID, "aone_id", aoneID, "nickname", aoneAssignee)
			} else {
				slog.Debug("aone sync: assignee nickname not found in workspace", "workspace_id", wsID, "aone_id", aoneID, "nickname", aoneAssignee)
			}
		}

		if err := s.createIssueFromAone(ctx, ws, item, dedupUUID, issueCreatorID); err != nil {
			result.Failed++
			detail := fmt.Sprintf("aone_id=%s: %v", aoneID, err)
			result.FailedDetails = append(result.FailedDetails, detail)
			slog.Warn("aone sync: failed to create issue", "workspace_id", wsID, "aone_id", aoneID, "title", item.Title, "error", err)
			continue
		}
		result.Created++
		slog.Info("aone sync: created issue", "workspace_id", wsID, "aone_id", aoneID, "title", item.Title)
	}

	slog.Info("aone sync: completed",
		"workspace_id", wsID,
		"total_fetched", result.TotalFetched,
		"created", result.Created,
		"skipped_by_ts", result.Skipped,
		"already_synced", result.AlreadySynced,
		"failed", result.Failed,
	)
	return result
}

func (s *AoneSyncService) createIssueFromAone(ctx context.Context, ws db.Workspace, item aoneWorkItem, dedupUUID pgtype.UUID, creatorID pgtype.UUID) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)
	issueNumber, err := qtx.IncrementIssueCounter(ctx, ws.ID)
	if err != nil {
		return fmt.Errorf("increment counter: %w", err)
	}

	status := mapAoneStatus(item.Status)
	priority := mapAonePriority(item.Priority)
	title := fmt.Sprintf("[AONE-%s] %s", item.ID.String(), item.Title)

	description := item.Body
	if description == "" {
		description = fmt.Sprintf("[Aone %s #%s]", item.Category, item.ID.String())
	}

	issue, err := qtx.CreateIssueWithOrigin(ctx, db.CreateIssueWithOriginParams{
		WorkspaceID:  ws.ID,
		Title:        title,
		Description:  pgtype.Text{String: description, Valid: true},
		Status:       status,
		Priority:     priority,
		CreatorType:  "member",
		CreatorID:    creatorID,
		Position:     0,
		Number:       issueNumber,
		OriginType:   pgtype.Text{String: "aone", Valid: true},
		OriginID:     dedupUUID,
	})
	if err != nil {
		return fmt.Errorf("create issue: %w", err)
	}

	_ = qtx.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
		IssueID:  issue.ID,
		UserType: "member",
		UserID:   creatorID,
		Reason:   "creator",
	})

	return tx.Commit(ctx)
}

func extractAoneProjectID(settings []byte) string {
	if settings == nil {
		return ""
	}
	var s map[string]any
	if err := json.Unmarshal(settings, &s); err != nil {
		return ""
	}
	v, ok := s["aone_project_id"]
	if !ok {
		return ""
	}
	str, ok := v.(string)
	if !ok {
		if num, ok := v.(float64); ok {
			return strconv.FormatFloat(num, 'f', 0, 64)
		}
		return ""
	}
	return str
}

func deriveDedupUUID(workspaceID pgtype.UUID, aoneWorkitemID string) pgtype.UUID {
	ns := uuid.UUID(workspaceID.Bytes)
	derived := uuid.NewSHA1(ns, []byte("aone:"+aoneWorkitemID))
	return pgtype.UUID{Bytes: derived, Valid: true}
}

func fetchAoneWorkItems(ctx context.Context, projectID string) ([]aoneWorkItem, error) {
	cmd := exec.CommandContext(ctx, "a1", "project", "workitem", "list",
		"--project", projectID,
		"--format", "json",
		"--columns", "id,subject,status,priority,categoryIdentifier,gmtCreate",
	)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("a1 exited %d: %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return nil, err
	}

	var items []aoneWorkItem
	if err := json.Unmarshal(out, &items); err != nil {
		// a1 might wrap in an object
		var wrapped struct {
			Items []aoneWorkItem `json:"items"`
			Data  []aoneWorkItem `json:"data"`
		}
		if err2 := json.Unmarshal(out, &wrapped); err2 != nil {
			return nil, fmt.Errorf("parse a1 output: %w (raw: %.200s)", err, string(out))
		}
		if len(wrapped.Items) > 0 {
			items = wrapped.Items
		} else {
			items = wrapped.Data
		}
	}

	return items, nil
}

type aoneWorkItemDetail struct {
	Description string
	Assignee    string
}

func fetchAoneWorkItemDetail(ctx context.Context, itemID string) (aoneWorkItemDetail, error) {
	cmd := exec.CommandContext(ctx, "a1", "project", "workitem", "get", itemID, "--format", "json")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return aoneWorkItemDetail{}, fmt.Errorf("a1 exited %d: %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return aoneWorkItemDetail{}, err
	}

	var raw struct {
		Description string `json:"description"`
		Fields      []struct {
			Identifier string `json:"identifier"`
			Label      string `json:"label"`
			Value      string `json:"value"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return aoneWorkItemDetail{}, fmt.Errorf("parse detail: %w", err)
	}

	detail := aoneWorkItemDetail{Description: raw.Description}
	for _, f := range raw.Fields {
		identifier := strings.ToLower(strings.TrimSpace(f.Identifier))
		label := strings.TrimSpace(f.Label)
		if identifier == "assignedto" || strings.Contains(label, "指派给") {
			detail.Assignee = strings.TrimSpace(f.Value)
			break
		}
	}
	return detail, nil
}

func mapAoneStatus(s string) string {
	lower := strings.ToLower(strings.TrimSpace(s))
	switch {
	case lower == "open" || lower == "reopen" || lower == "新建" || lower == "重新打开":
		return "todo"
	case lower == "in progress" || lower == "开发中" || lower == "实现中":
		return "in_progress"
	case lower == "done" || lower == "closed" || lower == "已完成" || lower == "已关闭":
		return "done"
	case lower == "cancelled" || lower == "已取消" || lower == "废弃":
		return "cancelled"
	case lower == "in review" || lower == "评审中" || lower == "测试中":
		return "in_review"
	default:
		return "backlog"
	}
}

func extractAoneSyncMinCreatedTs(settings []byte) int64 {
	if settings == nil {
		return 0
	}
	var s map[string]any
	if err := json.Unmarshal(settings, &s); err != nil {
		return 0
	}
	v, ok := s["aone_sync_min_created_ts"]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

func isAfterTimestamp(gmtCreate string, minTsMs int64) bool {
	if gmtCreate == "" {
		return false
	}
	t, err := time.ParseInLocation("2006-01-02 15:04", gmtCreate, time.Local)
	if err != nil {
		return false
	}
	return t.UnixMilli() >= minTsMs
}

func mapAonePriority(p string) string {
	lower := strings.ToLower(strings.TrimSpace(p))
	switch {
	case lower == "urgent" || lower == "紧急":
		return "urgent"
	case lower == "high" || lower == "高":
		return "high"
	case lower == "medium" || lower == "中":
		return "medium"
	case lower == "low" || lower == "低":
		return "low"
	default:
		return "none"
	}
}
