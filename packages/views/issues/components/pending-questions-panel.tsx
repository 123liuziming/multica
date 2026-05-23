"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, HelpCircle } from "lucide-react";
import { issueQuestionsOptions } from "@multica/core/questions";
import type { AgentQuestion } from "@multica/core/types";
import { useWorkspacePaths } from "@multica/core/paths";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

interface Props {
  issueId: string;
}

/**
 * Sidebar surface on the Issue detail page. Shows every AskUserQuestion
 * record tied to this issue (pending + resolved), with agent avatar and
 * type tag so the reader can scan who asked what without leaving the page.
 *
 * Hidden entirely when this issue has never had a question — the section
 * adds no signal otherwise.
 */
export function PendingQuestionsPanel({ issueId }: Props) {
  const { t } = useT("questions");
  const wsPaths = useWorkspacePaths();
  const allQuery = useQuery(issueQuestionsOptions(issueId, "all"));
  const all: AgentQuestion[] = allQuery.data ?? [];
  const [open, setOpen] = useState(true);

  if (all.length === 0) return null;

  const pending = all.filter((q) => q.status === "pending");
  const resolved = all.filter((q) => q.status !== "pending");

  // Sort: pending first (newest first), then resolved (newest first).
  const ordered = [...pending, ...resolved].sort((a, b) => {
    const aPending = a.status === "pending";
    const bPending = b.status === "pending";
    if (aPending !== bPending) return aPending ? -1 : 1;
    return b.created_at.localeCompare(a.created_at);
  });

  const allHref = `${wsPaths.questions()}?issueId=${encodeURIComponent(issueId)}`;

  return (
    <div>
      <button
        type="button"
        className={`mb-2 flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors hover:bg-accent/70 ${
          open ? "" : "text-muted-foreground hover:text-foreground"
        }`}
        onClick={() => setOpen((v) => !v)}
      >
        {t(($) => $.issue_panel_title)}
        {pending.length > 0 && (
          <span className="rounded-full bg-primary/15 px-1.5 text-[10px] font-medium text-primary">
            {pending.length}
          </span>
        )}
        {resolved.length > 0 && (
          <span className="rounded-full bg-muted px-1.5 text-[10px] font-medium text-muted-foreground">
            {resolved.length}
          </span>
        )}
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${
            open ? "rotate-90" : ""
          }`}
        />
      </button>
      {open && (
        <div className="space-y-1.5 pl-2">
          {ordered.map((q) => (
            <QuestionRow key={q.id} question={q} issueId={issueId} />
          ))}
          <AppLink
            href={allHref}
            className="block pt-1 text-xs text-muted-foreground hover:text-foreground"
          >
            {t(($) => $.issue_panel_view_all)} →
          </AppLink>
        </div>
      )}
    </div>
  );
}

function QuestionRow({
  question,
  issueId,
}: {
  question: AgentQuestion;
  issueId: string;
}) {
  const { t } = useT("questions");
  const wsPaths = useWorkspacePaths();
  const isPending = question.status === "pending";
  const tag = question.multi_select ? t(($) => $.tag_multi) : t(($) => $.tag_single);
  const href = `${wsPaths.questions()}?issueId=${encodeURIComponent(
    issueId,
  )}&agentId=${encodeURIComponent(question.agent_id)}`;
  return (
    <AppLink
      href={href}
      className={cn(
        "block rounded-md border bg-card/50 px-2 py-1.5 text-xs transition-colors hover:bg-accent/40",
        !isPending && "opacity-70",
      )}
    >
      <div className="flex items-start gap-1.5">
        <HelpCircle
          className={cn(
            "mt-0.5 h-3 w-3 shrink-0",
            isPending ? "text-primary" : "text-muted-foreground",
          )}
        />
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-1">
            <span
              className={cn(
                "shrink-0 rounded px-1 py-0.5 text-[9px] font-medium uppercase tracking-wider",
                question.multi_select
                  ? "bg-amber-500/15 text-amber-700 dark:text-amber-400"
                  : "bg-sky-500/15 text-sky-700 dark:text-sky-400",
              )}
            >
              [{tag}]
            </span>
            <span className="line-clamp-1 font-medium">{question.header}</span>
          </div>
          <div className="mt-0.5 flex items-center gap-1 text-[10px] text-muted-foreground">
            <ActorAvatar actorType="agent" actorId={question.agent_id} size={12} />
            <span className="truncate">
              {isPending
                ? t(($) => $.row_status_pending)
                : t(($) => $.row_status_resolved)}
            </span>
          </div>
        </div>
      </div>
    </AppLink>
  );
}
