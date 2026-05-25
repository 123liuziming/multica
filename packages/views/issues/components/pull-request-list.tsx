"use client";

import { useQuery } from "@tanstack/react-query";
import {
  GitPullRequest,
  GitPullRequestArrow,
  GitPullRequestClosed,
  GitMerge,
  GitPullRequestDraft,
  X,
} from "lucide-react";
import { issuePullRequestsOptions } from "@multica/core/github/queries";
import { useUnlinkPullRequest } from "@multica/core/github/mutations";
import type {
  PullRequest,
  PullRequestSource,
  PullRequestState,
} from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import { useT } from "../../i18n";

type IconConfig = { icon: React.ComponentType<{ className?: string }>; className: string };

// github gets the canonical PR colors. aone uses an amber palette so a
// reviewer scanning the panel can spot the source at a glance without a
// label. Unknown state always falls back to a generic outlined icon.
const GITHUB_STATE_ICON: Record<PullRequestState, IconConfig> = {
  open: { icon: GitPullRequestArrow, className: "text-emerald-600 dark:text-emerald-400" },
  draft: { icon: GitPullRequestDraft, className: "text-muted-foreground" },
  merged: { icon: GitMerge, className: "text-violet-600 dark:text-violet-400" },
  closed: { icon: GitPullRequestClosed, className: "text-rose-600 dark:text-rose-400" },
  unknown: { icon: GitPullRequest, className: "text-muted-foreground" },
};

const AONE_STATE_ICON: Record<PullRequestState, IconConfig> = {
  open: { icon: GitPullRequestArrow, className: "text-amber-600 dark:text-amber-400" },
  draft: { icon: GitPullRequestDraft, className: "text-muted-foreground" },
  merged: { icon: GitMerge, className: "text-amber-700 dark:text-amber-400" },
  closed: { icon: GitPullRequestClosed, className: "text-rose-600 dark:text-rose-400" },
  unknown: { icon: GitPullRequest, className: "text-muted-foreground" },
};

function pickIcon(source: PullRequestSource, state: PullRequestState): IconConfig {
  const table = source === "aone" ? AONE_STATE_ICON : GITHUB_STATE_ICON;
  return table[state] ?? table.unknown;
}

export function PullRequestList({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const { data, isLoading } = useQuery(issuePullRequestsOptions(issueId));
  const prs: PullRequest[] = data?.pull_requests ?? [];

  if (isLoading) {
    return <p className="text-xs text-muted-foreground px-2">{t(($) => $.detail.pull_requests_loading)}</p>;
  }
  if (prs.length === 0) {
    return (
      <p className="text-xs text-muted-foreground px-2">
        {t(($) => $.detail.pull_requests_empty)}
      </p>
    );
  }

  return (
    <div className="space-y-1">
      {prs.map((pr) => (
        <PullRequestRow key={pr.id} issueId={issueId} pr={pr} />
      ))}
    </div>
  );
}

function PullRequestRow({ issueId, pr }: { issueId: string; pr: PullRequest }) {
  const { t } = useT("issues");
  const unlink = useUnlinkPullRequest(issueId);
  // The server's enum may grow ahead of the client; default to the
  // generic fallback rather than crashing the row.
  const source = (pr.source === "aone" ? "aone" : "github") as PullRequestSource;
  const state = ((["open", "draft", "merged", "closed", "unknown"] as const).includes(
    pr.state as PullRequestState,
  )
    ? pr.state
    : "unknown") as PullRequestState;
  const cfg = pickIcon(source, state);
  const Icon = cfg.icon;

  const label =
    state === "open"
      ? t(($) => $.detail.pull_request_state_open)
      : state === "draft"
        ? t(($) => $.detail.pull_request_state_draft)
        : state === "merged"
          ? t(($) => $.detail.pull_request_state_merged)
          : state === "closed"
            ? t(($) => $.detail.pull_request_state_closed)
            : t(($) => $.detail.pull_request_state_unknown);

  const subline = buildSubline(pr, label);

  const handleUnlink = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    unlink.mutate(
      { source, prId: pr.id },
      {
        onSuccess: () => toast.success(t(($) => $.detail.unlink_pull_request_toast_success)),
        onError: () => toast.error(t(($) => $.detail.unlink_pull_request_toast_error)),
      },
    );
  };

  // Anchor + button are siblings, not nested — a <button> inside an <a> is
  // invalid HTML and triggers React's hydration warnings on top of breaking
  // pointer events on Safari.
  return (
    <div className="group flex items-start gap-2 rounded-md px-2 py-1.5 -mx-2 hover:bg-accent/50 transition-colors">
      <a
        href={pr.html_url}
        target="_blank"
        rel="noreferrer noopener"
        className="flex flex-1 min-w-0 items-start gap-2"
      >
        <Icon className={cn("h-3.5 w-3.5 mt-0.5 shrink-0", cfg.className)} />
        <div className="min-w-0 flex-1">
          <p className="text-xs font-medium truncate group-hover:text-foreground">{pr.title}</p>
          <p className="text-[11px] text-muted-foreground truncate">{subline}</p>
        </div>
      </a>
      <button
        type="button"
        aria-label={t(($) => $.detail.unlink_pull_request_aria)}
        onClick={handleUnlink}
        disabled={unlink.isPending}
        className="opacity-0 group-hover:opacity-100 focus-visible:opacity-100 disabled:opacity-50 transition-opacity rounded p-0.5 -mr-0.5 text-muted-foreground hover:text-foreground hover:bg-accent"
      >
        <X className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}

function buildSubline(pr: PullRequest, stateLabel: string): string {
  const parts: string[] = [];
  if (pr.repo_owner && pr.repo_name && pr.number != null) {
    // # for github, ! for aone — matches the convention each platform uses
    // in its own UI so reviewers don't have to translate.
    const sep = pr.source === "aone" ? "!" : "#";
    parts.push(`${pr.repo_owner}/${pr.repo_name}${sep}${pr.number}`);
  } else {
    try {
      parts.push(new URL(pr.html_url).host);
    } catch {
      parts.push(pr.html_url);
    }
  }
  parts.push(stateLabel);
  if (pr.author_login) parts.push(`@${pr.author_login}`);
  return parts.join(" · ");
}
