package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// QuestionOption mirrors a single option in an AskUserQuestion tool call.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// QuestionPayload is what Claude Code passes as the AskUserQuestion tool
// input (one batch can contain N questions).
type QuestionPayload struct {
	Question    string           `json:"question"`
	Header      string           `json:"header"`
	Options     []QuestionOption `json:"options"`
	MultiSelect bool             `json:"multiSelect"`
}

// askUserBatch is the body the hook script sends to the daemon — Claude's
// raw AskUserQuestion tool_input.
type askUserBatch struct {
	Questions []QuestionPayload `json:"questions"`
}

// createdQuestion is the server response per persisted question (mirrors the
// handler's QuestionResponse but daemon-side we only need id + indices map).
type createdQuestion struct {
	ID                  string           `json:"id"`
	Header              string           `json:"header"`
	Question            string           `json:"question"`
	Options             []QuestionOption `json:"options"`
	MultiSelect         bool             `json:"multi_select"`
	Status              string           `json:"status"`
	AnswerOptionIndices []int            `json:"answer_option_indices,omitempty"`
	AnswerCustomText    string           `json:"answer_custom_text,omitempty"`
}

// askUserResponse is what we return to the hook script. `formatted` contains
// the human-readable, model-consumable rendition of all answers.
type askUserResponse struct {
	Formatted string `json:"formatted"`
	Error     string `json:"error,omitempty"`
}

// registerQuestionRoutes wires `POST /question/ask` into the daemon's local
// health mux. The hook script (see execenv/ask_user_hook.go) is the only
// caller.
func (d *Daemon) registerQuestionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/question/ask", d.handleQuestionAsk)
}

func (d *Daemon) handleQuestionAsk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	taskID := r.Header.Get("X-Multica-Task-Id")
	if taskID == "" {
		writeJSONErr(w, http.StatusBadRequest, "X-Multica-Task-Id header required")
		return
	}

	var batch askUserBatch
	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if len(batch.Questions) == 0 {
		writeJSONErr(w, http.StatusBadRequest, "questions[] required")
		return
	}

	// Step 1: persist questions on the server.
	created, err := d.client.CreateTaskQuestions(r.Context(), taskID, batch.Questions)
	if err != nil {
		d.logger.Warn("question: create failed", "task_id", taskID, "error", err)
		writeJSONErr(w, http.StatusBadGateway, "create question failed: "+err.Error())
		return
	}
	if len(created) == 0 {
		writeJSONErr(w, http.StatusInternalServerError, "server returned no questions")
		return
	}

	// Step 2: wait for every question to terminate (answered/cancelled).
	// 24h hard cap mirrors the hook script's curl timeout; in practice the
	// task is usually answered or cancelled within minutes.
	waitCtx, cancel := context.WithTimeout(r.Context(), 24*time.Hour)
	defer cancel()
	answers := make([]createdQuestion, len(created))
	for i, q := range created {
		final, werr := d.client.WaitForQuestion(waitCtx, q.ID)
		if werr != nil {
			d.logger.Warn("question: wait failed", "task_id", taskID, "question_id", q.ID, "error", werr)
			writeJSONErr(w, http.StatusGatewayTimeout, "wait for question failed: "+werr.Error())
			return
		}
		answers[i] = final
	}

	// Step 3: format the answers into a single string the model can consume.
	resp := askUserResponse{Formatted: formatAnswers(answers)}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// formatAnswers turns N answered questions into one readable block that
// Claude will receive as the AskUserQuestion "tool result" via the
// PreToolUse hook's permissionDecisionReason.
func formatAnswers(answers []createdQuestion) string {
	var b strings.Builder
	if len(answers) == 1 {
		writeOne(&b, answers[0])
		return b.String()
	}
	for i, a := range answers {
		fmt.Fprintf(&b, "Q%d. ", i+1)
		writeOne(&b, a)
		if i != len(answers)-1 {
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

func writeOne(b *strings.Builder, a createdQuestion) {
	if a.Status == "cancelled" {
		fmt.Fprintf(b, "[cancelled] %s", a.Question)
		return
	}
	var parts []string
	for _, idx := range a.AnswerOptionIndices {
		if idx < 0 || idx >= len(a.Options) {
			continue
		}
		opt := a.Options[idx]
		if opt.Description != "" {
			parts = append(parts, fmt.Sprintf("%q (%s)", opt.Label, opt.Description))
		} else {
			parts = append(parts, fmt.Sprintf("%q", opt.Label))
		}
	}
	fmt.Fprintf(b, "Question: %s\n", a.Question)
	if len(parts) > 0 {
		fmt.Fprintf(b, "Selected: %s\n", strings.Join(parts, ", "))
	}
	if a.AnswerCustomText != "" {
		fmt.Fprintf(b, "User comment: %s", a.AnswerCustomText)
	}
}

// writeJSONErr is a small helper for the daemon HTTP handlers so we return
// errors as JSON instead of plain text — the hook script reads `.error`.
func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(askUserResponse{Error: msg})
}

