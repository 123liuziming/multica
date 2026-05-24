package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// QuestionOption mirrors a single option in an AskUserQuestion tool call.
// Both fields stay opaque strings — multica does not interpret them.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// QuestionResponse is the JSON shape returned to UI/API/CLI clients.
type QuestionResponse struct {
	ID                  string           `json:"id"`
	WorkspaceID         string           `json:"workspace_id"`
	TaskID              string           `json:"task_id"`
	AgentID             string           `json:"agent_id"`
	IssueID             *string          `json:"issue_id"`
	Header              string           `json:"header"`
	Question            string           `json:"question"`
	Options             []QuestionOption `json:"options"`
	MultiSelect         bool             `json:"multi_select"`
	Status              string           `json:"status"`
	AnswerOptionIndices []int            `json:"answer_option_indices,omitempty"`
	AnswerCustomText    string           `json:"answer_custom_text,omitempty"`
	AnsweredByUserID    *string          `json:"answered_by_user_id,omitempty"`
	AnsweredAt          *string          `json:"answered_at,omitempty"`
	CreatedAt           string           `json:"created_at"`
}

func questionToResponse(q db.AgentQuestion) QuestionResponse {
	opts := []QuestionOption{}
	if len(q.Options) > 0 {
		if err := json.Unmarshal(q.Options, &opts); err != nil {
			slog.Warn("failed to unmarshal agent_question.options", "id", uuidToString(q.ID), "error", err)
			opts = []QuestionOption{}
		}
	}
	var indices []int
	if len(q.AnswerOptionIndices) > 0 {
		if err := json.Unmarshal(q.AnswerOptionIndices, &indices); err != nil {
			slog.Warn("failed to unmarshal agent_question.answer_option_indices", "id", uuidToString(q.ID), "error", err)
		}
	}
	resp := QuestionResponse{
		ID:                  uuidToString(q.ID),
		WorkspaceID:         uuidToString(q.WorkspaceID),
		TaskID:              uuidToString(q.TaskID),
		AgentID:             uuidToString(q.AgentID),
		IssueID:             uuidToPtr(q.IssueID),
		Header:              q.Header,
		Question:            q.Question,
		Options:             opts,
		MultiSelect:         q.MultiSelect,
		Status:              q.Status,
		AnswerOptionIndices: indices,
		AnswerCustomText:    q.AnswerCustomText.String,
		AnsweredByUserID:    uuidToPtr(q.AnsweredByUserID),
		AnsweredAt:          timestampToPtr(q.AnsweredAt),
		CreatedAt:           timestampToString(q.CreatedAt),
	}
	return resp
}

// QuestionWaiter holds the per-question channel that long-poll waiters block
// on. The Server-side daemon long-poll handler waits on `done` for up to ~25s
// per request; AnswerAgentQuestion sends to `done` non-blockingly so a missed
// receive doesn't deadlock the answer flow (the next long-poll falls back to
// re-querying the DB and sees the answered state). One channel per question
// id is plenty — multiple concurrent waiters on the same question id is not a
// real scenario today.
type questionWaiter struct {
	done chan db.AgentQuestion
}

var (
	questionWaitersMu sync.Mutex
	questionWaiters   = map[string]*questionWaiter{}
)

func registerQuestionWaiter(id string) *questionWaiter {
	questionWaitersMu.Lock()
	defer questionWaitersMu.Unlock()
	w, ok := questionWaiters[id]
	if !ok {
		w = &questionWaiter{done: make(chan db.AgentQuestion, 1)}
		questionWaiters[id] = w
	}
	return w
}

func unregisterQuestionWaiter(id string) {
	questionWaitersMu.Lock()
	defer questionWaitersMu.Unlock()
	delete(questionWaiters, id)
}

func notifyQuestionWaiter(id string, q db.AgentQuestion) {
	questionWaitersMu.Lock()
	w, ok := questionWaiters[id]
	questionWaitersMu.Unlock()
	if !ok {
		return
	}
	select {
	case w.done <- q:
	default:
	}
}

// ---------- user-facing list/get/answer handlers ----------

// ListWorkspaceQuestions backs `GET /api/questions`. Workspace comes from the
// X-Workspace-ID header (same as ListAgents). Optional ?status filter limits
// to "pending" or "answered"; omit for all statuses.
func (h *Handler) ListWorkspaceQuestions(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	statusFilter := pgtype.Text{}
	if s := r.URL.Query().Get("status"); s != "" && s != "all" {
		statusFilter = pgtype.Text{String: s, Valid: true}
	}
	limit := int32(200)
	if s := r.URL.Query().Get("limit"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 && v <= 1000 {
			limit = int32(v)
		}
	}
	rows, err := h.Queries.ListWorkspaceQuestions(r.Context(), db.ListWorkspaceQuestionsParams{
		WorkspaceID:  wsUUID,
		StatusFilter: statusFilter,
		Limit:        limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list questions")
		return
	}
	resp := make([]QuestionResponse, len(rows))
	for i, q := range rows {
		resp[i] = questionToResponse(q)
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetQuestionPendingCounts backs `GET /api/questions/counts` — returns
// `{issue_id, pending}` aggregated for use by issue badges and the sidebar
// pending number.
func (h *Handler) GetQuestionPendingCounts(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	perIssue, err := h.Queries.CountPendingQuestionsByIssue(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count pending questions")
		return
	}
	total, err := h.Queries.CountWorkspacePendingQuestions(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count pending questions")
		return
	}

	type issueCount struct {
		IssueID string `json:"issue_id"`
		Pending int32  `json:"pending"`
	}
	out := struct {
		Total    int32        `json:"total"`
		PerIssue []issueCount `json:"per_issue"`
	}{Total: int32(total), PerIssue: make([]issueCount, 0, len(perIssue))}
	for _, row := range perIssue {
		out.PerIssue = append(out.PerIssue, issueCount{
			IssueID: uuidToString(row.IssueID),
			Pending: row.PendingCount,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// ListIssueQuestions backs `GET /api/issues/{id}/questions`.
func (h *Handler) ListIssueQuestions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	statusFilter := pgtype.Text{}
	if s := r.URL.Query().Get("status"); s != "" && s != "all" {
		statusFilter = pgtype.Text{String: s, Valid: true}
	}
	rows, err := h.Queries.ListIssueQuestions(r.Context(), db.ListIssueQuestionsParams{
		IssueID:      issue.ID,
		StatusFilter: statusFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list questions")
		return
	}
	resp := make([]QuestionResponse, len(rows))
	for i, q := range rows {
		resp[i] = questionToResponse(q)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ListAgentQuestions backs `GET /api/agents/{id}/questions`.
func (h *Handler) ListAgentQuestions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	statusFilter := pgtype.Text{}
	if s := r.URL.Query().Get("status"); s != "" && s != "all" {
		statusFilter = pgtype.Text{String: s, Valid: true}
	}
	limit := int32(200)
	if s := r.URL.Query().Get("limit"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 && v <= 1000 {
			limit = int32(v)
		}
	}
	rows, err := h.Queries.ListAgentQuestions(r.Context(), db.ListAgentQuestionsParams{
		AgentID:      agent.ID,
		StatusFilter: statusFilter,
		Limit:        limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list questions")
		return
	}
	resp := make([]QuestionResponse, len(rows))
	for i, q := range rows {
		resp[i] = questionToResponse(q)
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetQuestion backs `GET /api/questions/{id}`.
func (h *Handler) GetQuestion(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	q, ok := h.loadQuestionForUser(w, r, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, questionToResponse(q))
}

// AnswerQuestionRequest is the wire shape for `POST /api/questions/{id}/answer`.
type AnswerQuestionRequest struct {
	OptionIndices []int  `json:"option_indices"`
	CustomText    string `json:"custom_text"`
}

// AnswerQuestion backs `POST /api/questions/{id}/answer`. MVP has no role gate —
// any workspace member can answer — but we record answered_by_user_id for audit.
func (h *Handler) AnswerQuestion(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	q, ok := h.loadQuestionForUser(w, r, id)
	if !ok {
		return
	}
	if q.Status != "pending" {
		writeError(w, http.StatusConflict, "question is no longer pending")
		return
	}

	var req AnswerQuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.OptionIndices) == 0 && req.CustomText == "" {
		writeError(w, http.StatusBadRequest, "either option_indices or custom_text is required")
		return
	}

	// Decode options to validate indices.
	var opts []QuestionOption
	if len(q.Options) > 0 {
		_ = json.Unmarshal(q.Options, &opts)
	}
	for _, idx := range req.OptionIndices {
		if idx < 0 || idx >= len(opts) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("option index %d out of range", idx))
			return
		}
	}
	if !q.MultiSelect && len(req.OptionIndices) > 1 {
		writeError(w, http.StatusBadRequest, "this question accepts at most one option")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	indicesBytes, _ := json.Marshal(req.OptionIndices)

	answered, err := h.Queries.AnswerAgentQuestion(r.Context(), db.AnswerAgentQuestionParams{
		ID:                  q.ID,
		AnswerOptionIndices: indicesBytes,
		AnswerCustomText:    pgtype.Text{String: req.CustomText, Valid: req.CustomText != ""},
		AnsweredByUserID:    parseUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "question is no longer pending")
			return
		}
		slog.Warn("answer question failed", append(logger.RequestAttrs(r), "error", err, "question_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to answer question")
		return
	}

	// Wake any daemon long-poll waiting on this id. Best-effort: a missed
	// notify is fine because the long-poll re-queries the DB on each cycle.
	notifyQuestionWaiter(uuidToString(answered.ID), answered)

	resp := questionToResponse(answered)
	actorType, actorID := h.resolveActor(r, userID, uuidToString(answered.WorkspaceID))
	workspaceID := uuidToString(answered.WorkspaceID)
	payload := map[string]any{
		"question": resp,
	}

	writeJSON(w, http.StatusOK, resp)

	// Card callbacks should only wait for the database state transition and
	// daemon wake-up. Activity, realtime, and changelog listeners are best
	// effort side effects and may involve slower I/O.
	go h.publish(protocol.EventQuestionAnswered, workspaceID, actorType, actorID, payload)
}

func (h *Handler) loadQuestionForUser(w http.ResponseWriter, r *http.Request, qid string) (db.AgentQuestion, bool) {
	if _, ok := requireUserID(w, r); !ok {
		return db.AgentQuestion{}, false
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.AgentQuestion{}, false
	}
	qUUID, ok := parseUUIDOrBadRequest(w, qid, "question id")
	if !ok {
		return db.AgentQuestion{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.AgentQuestion{}, false
	}
	q, err := h.Queries.GetAgentQuestionInWorkspace(r.Context(), db.GetAgentQuestionInWorkspaceParams{
		ID:          qUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "question not found")
		return db.AgentQuestion{}, false
	}
	return q, true
}

// cancelPendingQuestionsForTask is invoked by the daemon-facing task
// terminal handlers (Complete/Fail/Cancel) so the hook script's long-poll
// receives a definitive answer instead of hanging until 24h timeout.
// Best-effort; logs and moves on if the cancel SQL returns an error.
func (h *Handler) cancelPendingQuestionsForTask(ctx context.Context, task db.AgentTaskQueue) {
	rows, err := h.Queries.CancelPendingQuestionsForTask(ctx, task.ID)
	if err != nil {
		slog.Warn("cancel pending questions failed", "task_id", uuidToString(task.ID), "error", err)
		return
	}
	for _, q := range rows {
		notifyQuestionWaiter(uuidToString(q.ID), q)
		h.publish(protocol.EventQuestionCancelled, uuidToString(q.WorkspaceID), "system", "", map[string]any{
			"question": questionToResponse(q),
		})
	}
}

// DeletePendingQuestionsForTask removes pending agent_question rows
// belonging to a task that's being cancelled mid-way (chat session cancel,
// agent archive, bulk cancel, trigger-comment delete, runtime revoke,
// claim-time isolation failure). Wakes any active long-poll with an
// in-memory cancelled payload so the daemon hook returns cleanly, then
// publishes EventQuestionCancelled for UI invalidation.
//
// Why delete (not cancel-and-mark) here: these are aborted attempts, not
// historical Q&A worth surfacing in the resolved list. The Complete/Fail
// terminal paths still go through cancelPendingQuestionsForTask which
// preserves the row.
func (h *Handler) deletePendingQuestionsForTask(ctx context.Context, taskID pgtype.UUID) {
	rows, err := h.Queries.DeletePendingQuestionsForTask(ctx, taskID)
	if err != nil {
		slog.Warn("delete pending questions failed", "task_id", uuidToString(taskID), "error", err)
		return
	}
	for _, q := range rows {
		// In-memory only: row is already gone from DB; we still want the
		// daemon's blocked long-poll to receive a terminal payload so it
		// can format `[cancelled]` for the hook instead of timing out.
		q.Status = "cancelled"
		notifyQuestionWaiter(uuidToString(q.ID), q)
		h.publish(protocol.EventQuestionCancelled, uuidToString(q.WorkspaceID), "system", "", map[string]any{
			"question": questionToResponse(q),
		})
	}
}

// ---------- daemon-facing endpoints ----------

// CreateTaskQuestionsRequest is the wire shape posted by the daemon
// `/question/ask` HTTP bridge. Mirrors Claude Code's AskUserQuestion tool
// input shape so the hook script can forward stdin verbatim.
type CreateTaskQuestionsRequest struct {
	Questions []struct {
		Question    string           `json:"question"`
		Header      string           `json:"header"`
		Options     []QuestionOption `json:"options"`
		MultiSelect bool             `json:"multiSelect"`
	} `json:"questions"`
}

// CreateTaskQuestions backs `POST /api/daemon/tasks/{taskId}/questions`.
// Reuses an identical pending/answered issue question when one already exists,
// otherwise persists a new row, broadcasts question:created, and returns the
// records so the daemon knows which IDs to long-poll for.
//
// Workspace scoping is enforced via requireDaemonTaskAccess — the caller's
// daemon/PAT must own the task's workspace. We do NOT re-check
// agent.AllowAskUserQuestion at runtime: the gate is on hook injection at
// dispatch time, and in-flight tasks keep whatever was wired in then. A
// user toggling the flag mid-run would otherwise 403 already-running
// agents, breaking the "config snapshot at dispatch" contract.
func (h *Handler) CreateTaskQuestions(w http.ResponseWriter, r *http.Request) {
	taskIDStr := chi.URLParam(r, "taskId")
	task, ok := h.requireDaemonTaskAccess(w, r, taskIDStr)
	if !ok {
		return
	}
	if task.Status != "dispatched" && task.Status != "running" {
		writeError(w, http.StatusConflict, "task is not in flight")
		return
	}
	agent, err := h.Queries.GetAgent(r.Context(), task.AgentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	var req CreateTaskQuestionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Questions) == 0 {
		writeError(w, http.StatusBadRequest, "questions[] is required")
		return
	}

	workspaceID := uuidToString(agent.WorkspaceID)
	created := make([]QuestionResponse, 0, len(req.Questions))
	createdAny := false
	for _, q := range req.Questions {
		header := q.Header
		if header == "" {
			header = "Question"
		}
		optsBytes, err := json.Marshal(q.Options)
		if err != nil {
			optsBytes = []byte("[]")
		}
		if task.IssueID.Valid {
			existing, err := h.Queries.FindMatchingIssueQuestion(r.Context(), db.FindMatchingIssueQuestionParams{
				WorkspaceID: agent.WorkspaceID,
				IssueID:     task.IssueID,
				Header:      header,
				Question:    q.Question,
				Options:     optsBytes,
				MultiSelect: q.MultiSelect,
			})
			if err == nil {
				created = append(created, questionToResponse(existing))
				continue
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				slog.Warn("find matching agent_question failed", "task_id", taskIDStr, "error", err)
				writeError(w, http.StatusInternalServerError, "failed to find matching question")
				return
			}
		}
		row, err := h.Queries.CreateAgentQuestion(r.Context(), db.CreateAgentQuestionParams{
			WorkspaceID: agent.WorkspaceID,
			TaskID:      task.ID,
			AgentID:     agent.ID,
			IssueID:     task.IssueID,
			Header:      header,
			Question:    q.Question,
			Options:     optsBytes,
			MultiSelect: q.MultiSelect,
		})
		if err != nil {
			slog.Warn("create agent_question failed", "task_id", taskIDStr, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create question")
			return
		}
		createdAny = true
		resp := questionToResponse(row)
		created = append(created, resp)

		h.publish(protocol.EventQuestionCreated, workspaceID, "agent", uuidToString(agent.ID), map[string]any{
			"question": resp,
		})
	}
	status := http.StatusOK
	if createdAny {
		status = http.StatusCreated
	}
	writeJSON(w, status, created)
}

// WaitForQuestion backs `GET /api/daemon/questions/{id}/wait`. Long-polls
// for up to ~25s waiting for the question to be answered or cancelled.
// Returns 200 + the question body when terminal, 204 + same body when still
// pending so the daemon can re-poll. The daemon controls overall timeout
// budget by re-issuing this request in a loop (default total 24h).
//
// Workspace scoping: after loading the question we resolve its task and
// gate via requireDaemonWorkspaceAccess. Without this, any authenticated
// daemon/PAT could long-poll a question UUID belonging to a foreign
// workspace and learn its answer.
func (h *Handler) WaitForQuestion(w http.ResponseWriter, r *http.Request) {
	qid := chi.URLParam(r, "id")
	qUUID, ok := parseUUIDOrBadRequest(w, qid, "question id")
	if !ok {
		return
	}
	q, err := h.Queries.GetAgentQuestion(r.Context(), qUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "question not found")
		return
	}
	if !h.requireDaemonWorkspaceAccess(w, r, uuidToString(q.WorkspaceID)) {
		return
	}
	if q.Status != "pending" {
		writeJSON(w, http.StatusOK, questionToResponse(q))
		return
	}

	waiter := registerQuestionWaiter(qid)
	defer unregisterQuestionWaiter(qid)

	timeout := 25 * time.Second
	if s := r.URL.Query().Get("timeout"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 && v <= 60 {
			timeout = time.Duration(v) * time.Second
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	select {
	case updated := <-waiter.done:
		writeJSON(w, http.StatusOK, questionToResponse(updated))
	case <-ctx.Done():
		// Re-query in case the answer landed during the short race window
		// between channel send and the receive deadline.
		latest, lerr := h.Queries.GetAgentQuestion(r.Context(), qUUID)
		if lerr == nil && latest.Status != "pending" {
			writeJSON(w, http.StatusOK, questionToResponse(latest))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNoContent)
	}
}
