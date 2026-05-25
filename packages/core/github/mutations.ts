import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { PullRequest } from "../types";
import { githubKeys } from "./queries";

type PullRequestListData = { pull_requests: PullRequest[] };

/** Link a manual PR/MR URL to an issue. */
export function useLinkPullRequest(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { url: string; title?: string }) => api.linkPullRequest(issueId, body),
    onSuccess: (pr: PullRequest) => {
      qc.setQueryData<PullRequestListData>(githubKeys.pullRequests(issueId), (prev) => {
        // Mirror useAttachLabel's "no prev, no patch" rule. Inserting into an
        // empty cache turns a render-from-fetch path into a render-from-stale
        // path and the user sees the wrong row count for a frame.
        if (!prev) return prev;
        if (prev.pull_requests.some((p) => p.id === pr.id)) return prev;
        return { ...prev, pull_requests: [...prev.pull_requests, pr] };
      });
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: githubKeys.pullRequests(issueId) });
    },
  });
}

/** Unlink a PR from an issue. Optimistic; rolls back on error. */
export function useUnlinkPullRequest(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ source, prId }: { source: PullRequest["source"]; prId: string }) =>
      api.unlinkPullRequest(issueId, source, prId),
    onMutate: async ({ prId }) => {
      await qc.cancelQueries({ queryKey: githubKeys.pullRequests(issueId) });
      const prev = qc.getQueryData<PullRequestListData>(githubKeys.pullRequests(issueId));
      if (!prev) return { prev };
      const next: PullRequestListData = {
        ...prev,
        pull_requests: prev.pull_requests.filter((p) => p.id !== prId),
      };
      qc.setQueryData<PullRequestListData>(githubKeys.pullRequests(issueId), next);
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(githubKeys.pullRequests(issueId), ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: githubKeys.pullRequests(issueId) });
    },
  });
}
