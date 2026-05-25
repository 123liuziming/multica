import { queryOptions, useQuery } from "@tanstack/react-query";
import { api } from "../api";
import type { AgentQuestion, QuestionCountsResponse, QuestionStatus } from "../types";

/**
 * Query keys for agent_question reads. Keyed by workspace so a workspace
 * switch automatically routes queries through the correct cache slot (the
 * `wsId` change makes TanStack Query treat them as different queries).
 *
 * Per-issue / per-agent lists are NOT keyed by wsId — the issue / agent UUID
 * is globally unique, and the WS event handler doesn't have a wsId for
 * `question:*` payloads without an extra round-trip. Keep the entity IDs as
 * the only key components.
 */
export const questionKeys = {
  all: (wsId: string) => ["questions", wsId] as const,
  list: (wsId: string, status?: QuestionStatus | "all") =>
    [...questionKeys.all(wsId), "list", status ?? "all"] as const,
  counts: (wsId: string) => [...questionKeys.all(wsId), "counts"] as const,
  detail: (id: string) => ["questions", "detail", id] as const,
  byIssue: (issueId: string, status?: QuestionStatus | "all") =>
    ["questions", "issue", issueId, status ?? "all"] as const,
  /** Prefix-match helper for invalidating every per-issue list on a single
   *  issue regardless of status filter. */
  byIssueAll: (issueId: string) => ["questions", "issue", issueId] as const,
  byAgent: (agentId: string, status?: QuestionStatus | "all") =>
    ["questions", "agent", agentId, status ?? "all"] as const,
  byAgentAll: (agentId: string) => ["questions", "agent", agentId] as const,
};

export function workspaceQuestionsOptions(
  wsId: string,
  status: QuestionStatus | "all" = "pending",
) {
  return queryOptions({
    queryKey: questionKeys.list(wsId, status),
    queryFn: () => api.listWorkspaceQuestions({ status, limit: 200 }),
  });
}

export function questionCountsOptions(wsId: string) {
  return queryOptions({
    queryKey: questionKeys.counts(wsId),
    queryFn: () => api.getQuestionCounts(),
  });
}

export function issueQuestionsOptions(
  issueId: string,
  status: QuestionStatus | "all" = "all",
) {
  return queryOptions({
    queryKey: questionKeys.byIssue(issueId, status),
    queryFn: () => api.listIssueQuestions(issueId, { status }),
  });
}

export function agentQuestionsOptions(
  agentId: string,
  status: QuestionStatus | "all" = "all",
) {
  return queryOptions({
    queryKey: questionKeys.byAgent(agentId, status),
    queryFn: () => api.listAgentQuestions(agentId, { status, limit: 200 }),
  });
}

/** Sidebar pending-question badge — single count. Returns 0 while loading. */
export function useWorkspacePendingQuestionCount(
  wsId: string | null | undefined,
): number {
  const { data } = useQuery({
    queryKey: questionKeys.counts(wsId ?? ""),
    queryFn: () => api.getQuestionCounts(),
    enabled: !!wsId,
    select: (counts: QuestionCountsResponse) => counts.total,
  });
  return data ?? 0;
}

/** Issue-detail "Pending Questions" panel count. */
export function useIssuePendingQuestions(
  issueId: string | null | undefined,
): AgentQuestion[] {
  const { data } = useQuery({
    queryKey: questionKeys.byIssue(issueId ?? "", "pending"),
    queryFn: () => api.listIssueQuestions(issueId ?? "", { status: "pending" }),
    enabled: !!issueId,
  });
  return data ?? [];
}

/** Agent-detail "Pending Questions" badge / panel. */
export function useAgentPendingQuestions(
  agentId: string | null | undefined,
): AgentQuestion[] {
  const { data } = useQuery({
    queryKey: questionKeys.byAgent(agentId ?? "", "pending"),
    queryFn: () => api.listAgentQuestions(agentId ?? "", { status: "pending" }),
    enabled: !!agentId,
  });
  return data ?? [];
}
