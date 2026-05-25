package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dingtalk"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const questionCardSendTimeout = 10 * time.Second
const questionCardSessionKeyEnv = "CC_CONNECT_QUESTION_CARD_SESSION_KEY"

// registerQuestionCardListeners sends issue-scoped agent questions to
// cc-connect so it can render DingTalk cards and answer through the CLI
// callback path.
func registerQuestionCardListeners(bus *events.Bus, queries *db.Queries, cc *dingtalk.CCConnectClient) {
	if !cc.Enabled() {
		return
	}

	bus.Subscribe(protocol.EventQuestionCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		question, ok := payload["question"].(handler.QuestionResponse)
		if !ok {
			return
		}
		if question.WorkspaceID == "" {
			question.WorkspaceID = e.WorkspaceID
		}
		if question.IssueID == nil || *question.IssueID == "" {
			return
		}
		if question.Status != "" && question.Status != "pending" {
			return
		}

		go sendIssueQuestionCards(context.Background(), queries, cc, question)
	})
}

func sendIssueQuestionCards(ctx context.Context, queries *db.Queries, cc *dingtalk.CCConnectClient, question handler.QuestionResponse) {
	issueID := ""
	if question.IssueID != nil {
		issueID = *question.IssueID
	}
	if issueID == "" || !cc.Enabled() {
		return
	}

	issue, err := queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		slog.Warn("question card: failed to load issue", "issue_id", issueID, "question_id", question.ID, "error", err)
		return
	}

	agentName := ""
	if question.AgentID != "" {
		if agent, err := queries.GetAgent(ctx, parseUUID(question.AgentID)); err == nil {
			agentName = agent.Name
		} else {
			slog.Warn("question card: failed to load agent", "agent_id", question.AgentID, "question_id", question.ID, "error", err)
		}
	}

	issueIdentifier := ""
	workspaceSlug := ""
	if ws, err := queries.GetWorkspace(ctx, issue.WorkspaceID); err == nil {
		workspaceSlug = ws.Slug
		if ws.IssuePrefix != "" && issue.Number > 0 {
			issueIdentifier = fmt.Sprintf("%s-%d", ws.IssuePrefix, issue.Number)
		}
	}
	questionURL := buildQuestionURL(workspaceSlug, issueID, question.ID)

	sessionKey := strings.TrimSpace(os.Getenv(questionCardSessionKeyEnv))
	if sessionKey != "" {
		req := buildQuestionCardRequest(question, issue, issueIdentifier, questionURL, agentName, "", "", sessionKey)
		sendCtx, cancel := context.WithTimeout(ctx, questionCardSendTimeout)
		err := cc.SendQuestionCard(sendCtx, req)
		cancel()
		if err != nil {
			slog.Warn("question card: cc-connect group send failed",
				"workspace_id", question.WorkspaceID,
				"issue_id", issueID,
				"question_id", question.ID,
				"session_key", sessionKey,
				"error", err)
			return
		}
		slog.Info("question card: sent group card via cc-connect",
			"workspace_id", question.WorkspaceID,
			"issue_id", issueID,
			"question_id", question.ID,
			"session_key", sessionKey)
		return
	}

	subscribers, err := queries.ListIssueSubscribers(ctx, parseUUID(issueID))
	if err != nil {
		slog.Warn("question card: failed to list issue subscribers", "issue_id", issueID, "question_id", question.ID, "error", err)
		return
	}

	recipients := map[string]string{}
	for _, sub := range subscribers {
		if sub.UserType != "member" {
			continue
		}
		recipientUserID := util.UUIDToString(sub.UserID)
		user, err := queries.GetUser(ctx, sub.UserID)
		if err != nil {
			slog.Warn("question card: failed to load subscriber user", "user_id", recipientUserID, "question_id", question.ID, "error", err)
			continue
		}
		dingUserID, ok := dingtalk.UserIDFromAlibabaEmail(user.Email)
		if !ok {
			slog.Debug("question card: subscriber email is not a DingTalk staff email", "user_id", recipientUserID, "email", user.Email)
			continue
		}
		if _, exists := recipients[dingUserID]; !exists {
			recipients[dingUserID] = recipientUserID
		}
	}

	for dingUserID, recipientUserID := range recipients {
		req := buildQuestionCardRequest(question, issue, issueIdentifier, questionURL, agentName, recipientUserID, dingUserID, "")
		sendCtx, cancel := context.WithTimeout(ctx, questionCardSendTimeout)
		err := cc.SendQuestionCard(sendCtx, req)
		cancel()
		if err != nil {
			slog.Warn("question card: cc-connect send failed",
				"workspace_id", question.WorkspaceID,
				"issue_id", issueID,
				"question_id", question.ID,
				"recipient_user_id", recipientUserID,
				"ding_user_id", dingUserID,
				"error", err)
			continue
		}
		slog.Info("question card: sent via cc-connect",
			"workspace_id", question.WorkspaceID,
			"issue_id", issueID,
			"question_id", question.ID,
			"recipient_user_id", recipientUserID,
			"ding_user_id", dingUserID)
	}
}

func buildQuestionCardRequest(
	question handler.QuestionResponse,
	issue db.Issue,
	issueIdentifier string,
	questionURL string,
	agentName string,
	recipientUserID string,
	dingUserID string,
	sessionKey string,
) dingtalk.QuestionCardRequest {
	issueID := ""
	if question.IssueID != nil {
		issueID = *question.IssueID
	}
	workspaceID := question.WorkspaceID
	if workspaceID == "" {
		workspaceID = util.UUIDToString(issue.WorkspaceID)
	}

	options := make([]dingtalk.QuestionCardOption, 0, len(question.Options))
	for i, opt := range question.Options {
		value := strconv.Itoa(i)
		options = append(options, dingtalk.QuestionCardOption{
			Index:       i,
			Value:       value,
			Label:       opt.Label,
			Description: opt.Description,
		})
	}

	metadata := map[string]string{
		"workspace_id":      workspaceID,
		"issue_id":          issueID,
		"issueTitle":        issue.Title,
		"questionUrl":       questionURL,
		"question_id":       question.ID,
		"question":          question.Question,
		"question_status":   question.Status,
		"task_id":           question.TaskID,
		"agent_id":          question.AgentID,
		"agent_name":        agentName,
		"agentName":         agentName,
		"user_id":           recipientUserID,
		"recipient_user_id": recipientUserID,
		"ding_user_id":      dingUserID,
		"card_schema_id":    dingtalk.QuestionCardSchemaID,
		"callback_action":   "multica.question.answer",
		"multi_select":      strconv.FormatBool(question.MultiSelect),
	}
	if question.Header != "" {
		metadata["header"] = question.Header
	}
	if question.CreatedAt != "" {
		metadata["created_at"] = question.CreatedAt
	}
	if issueIdentifier != "" {
		metadata["issue_identifier"] = issueIdentifier
	}
	if questionURL == "" {
		delete(metadata, "questionUrl")
	}
	if sessionKey != "" {
		metadata["session_key"] = sessionKey
	}

	return dingtalk.QuestionCardRequest{
		Platform:   "dingtalk",
		UserID:     dingUserID,
		SessionKey: sessionKey,
		SchemaID:   dingtalk.QuestionCardSchemaID,
		CardData: dingtalk.QuestionCardData{
			WorkspaceID:     workspaceID,
			IssueID:         issueID,
			IssueTitle:      issue.Title,
			IssueIdentifier: issueIdentifier,
			QuestionURL:     questionURL,
			QuestionID:      question.ID,
			Question:        question.Question,
			UserID:          recipientUserID,
			SessionKey:      sessionKey,
			TaskID:          question.TaskID,
			AgentID:         question.AgentID,
			AgentName:       agentName,
			AgentNameText:   agentName,
			Header:          question.Header,
			Options:         options,
			MultiSelect:     question.MultiSelect,
			CreatedAt:       question.CreatedAt,
		},
		Metadata: metadata,
	}
}

func buildQuestionURL(workspaceSlug, issueID, questionID string) string {
	if workspaceSlug == "" || issueID == "" || questionID == "" {
		return ""
	}
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN")), "/")
	if baseURL == "" {
		baseURL = "https://app.multica.ai"
	}
	values := url.Values{}
	values.Set("issueId", issueID)
	values.Set("questionId", questionID)
	return fmt.Sprintf("%s/%s/questions?%s", baseURL, url.PathEscape(workspaceSlug), values.Encode())
}
