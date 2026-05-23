/**
 * Agent-asked question records — the persisted side of Claude Code's
 * AskUserQuestion tool. See server/internal/handler/agent_question.go for
 * the wire shape; this mirror is what UI/API/CLI consumers see.
 */

export type QuestionStatus = "pending" | "answered" | "cancelled";

export interface QuestionOption {
  label: string;
  description?: string;
}

export interface AgentQuestion {
  id: string;
  workspace_id: string;
  task_id: string;
  agent_id: string;
  issue_id: string | null;
  /** Short label (Claude calls it `header`). Used as the question's title in
   *  lists. */
  header: string;
  /** Full question prompt body. */
  question: string;
  options: QuestionOption[];
  multi_select: boolean;
  status: QuestionStatus;
  /** Indices into `options` selected by the user when answered. */
  answer_option_indices?: number[];
  /** Free-form text the user typed alongside (or instead of) option picks. */
  answer_custom_text?: string;
  answered_by_user_id?: string | null;
  answered_at?: string | null;
  created_at: string;
}

/** `GET /api/questions/counts` shape. Powers both sidebar and per-issue badges. */
export interface QuestionCountsResponse {
  total: number;
  per_issue: Array<{ issue_id: string; pending: number }>;
}

export interface AnswerQuestionRequest {
  /** 0-indexed option positions; empty when custom_text is the answer. */
  option_indices?: number[];
  custom_text?: string;
}
