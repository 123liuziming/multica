import type { QueryClient } from "@tanstack/react-query";
import { questionKeys } from "./queries";
import type { AgentQuestion } from "../types";

/**
 * WebSocket-driven cache invalidation for the question:* event family.
 *
 * Each event fires a coarse invalidate across the workspace question
 * namespace plus the per-issue / per-agent caches that may show the same
 * row. Coarse is fine — the question list endpoints are small (capped at
 * 200) and the cost of one extra GET beats the maintenance burden of N
 * surgical `setQueryData` updaters.
 *
 * The per-task list (questionKeys.detail) is also invalidated so a
 * QuestionCard observing the detail of a freshly-answered question rerenders
 * with the server-truth state (catches the case where two browsers race on
 * the answer flow).
 */
export function onQuestionEvent(
  qc: QueryClient,
  wsId: string,
  q: AgentQuestion | null | undefined,
) {
  qc.invalidateQueries({ queryKey: questionKeys.all(wsId) });
  if (!q) return;
  qc.invalidateQueries({ queryKey: questionKeys.detail(q.id) });
  if (q.issue_id) {
    qc.invalidateQueries({ queryKey: questionKeys.byIssueAll(q.issue_id) });
  }
  if (q.agent_id) {
    qc.invalidateQueries({ queryKey: questionKeys.byAgentAll(q.agent_id) });
  }
}
