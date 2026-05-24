package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestAnswerQuestionReturnsBeforeEventListenersFinish(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Async Answer Agent", nil)
	taskID := createHandlerTestTaskForAgent(t, agentID)
	options, err := json.Marshal([]QuestionOption{{
		Label:       "Ship it",
		Description: "Proceed with the current answer",
	}})
	if err != nil {
		t.Fatalf("marshal question options: %v", err)
	}

	q, err := testHandler.Queries.CreateAgentQuestion(context.Background(), db.CreateAgentQuestionParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		TaskID:      parseUUID(taskID),
		AgentID:     parseUUID(agentID),
		Header:      "Confirm",
		Question:    "Should I proceed?",
		Options:     options,
		MultiSelect: false,
	})
	if err != nil {
		t.Fatalf("create question: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_question WHERE id = $1`, q.ID)
	})

	bus := events.New()
	listenerEntered := make(chan struct{})
	releaseListener := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseListener) }) }
	defer release()
	bus.Subscribe(protocol.EventQuestionAnswered, func(events.Event) {
		close(listenerEntered)
		<-releaseListener
	})

	h := &Handler{Queries: testHandler.Queries, Bus: bus}
	req := newRequest(http.MethodPost, "/api/questions/"+uuidToString(q.ID)+"/answer", AnswerQuestionRequest{
		OptionIndices: []int{0},
	})
	req = withURLParam(req, "id", uuidToString(q.ID))
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.AnswerQuestion(w, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		release()
		t.Fatal("AnswerQuestion blocked on question:answered listeners")
	}

	if w.Code != http.StatusOK {
		t.Fatalf("AnswerQuestion status=%d body=%s", w.Code, w.Body.String())
	}

	select {
	case <-listenerEntered:
	case <-time.After(time.Second):
		t.Fatal("question:answered event was not published")
	}
}
