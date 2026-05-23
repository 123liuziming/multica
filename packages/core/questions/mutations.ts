import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type { AgentQuestion, AnswerQuestionRequest } from "../types";
import { questionKeys } from "./queries";

/**
 * Answer a pending question. Optimistically flips the cached question to
 * `answered` so the UI moves it to the Resolved section instantly; on
 * settle, invalidates the workspace and per-issue/per-agent caches plus
 * the workspace pending-count so the sidebar badge updates.
 */
export function useAnswerQuestion() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: AnswerQuestionRequest }) =>
      api.answerQuestion(id, data),
    onMutate: async ({ id, data }) => {
      // Optimistic detail update so the QuestionCard re-renders immediately.
      const detailKey = questionKeys.detail(id);
      await qc.cancelQueries({ queryKey: detailKey });
      const prev = qc.getQueryData<AgentQuestion>(detailKey);
      if (prev) {
        qc.setQueryData<AgentQuestion>(detailKey, {
          ...prev,
          status: "answered",
          answer_option_indices: data.option_indices ?? [],
          answer_custom_text: data.custom_text ?? "",
          answered_at: new Date().toISOString(),
        });
      }
      return { prev };
    },
    onError: (_err, { id }, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(questionKeys.detail(id), ctx.prev);
      }
    },
    onSettled: (answered) => {
      if (wsId) {
        qc.invalidateQueries({ queryKey: questionKeys.all(wsId) });
      }
      if (answered) {
        qc.invalidateQueries({ queryKey: questionKeys.detail(answered.id) });
        if (answered.issue_id) {
          qc.invalidateQueries({ queryKey: questionKeys.byIssueAll(answered.issue_id) });
        }
        qc.invalidateQueries({ queryKey: questionKeys.byAgentAll(answered.agent_id) });
      }
    },
  });
}
