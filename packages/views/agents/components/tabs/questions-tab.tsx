"use client";

import { HelpCircle } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useAgentPendingQuestions } from "@multica/core/questions";
import { useWorkspacePaths } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueListOptions } from "@multica/core/issues/queries";
import type { Agent, AgentQuestion, Issue } from "@multica/core/types";
import { AppLink } from "../../../navigation";
import { useT } from "../../../i18n";

interface Props {
  agent: Agent;
}

const TYPE_TAG_CLASSES =
  "inline-block rounded px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wider";

/**
 * Pending-only listing of an agent's AskUserQuestion items. Each row shows
 * the type tag, header, and (when the question is tied to an issue) the
 * issue identifier as a separately-clickable chip — so the row supports
 * two destinations from one card.
 */
export function QuestionsTab({ agent }: Props) {
  const { t } = useT("agents");
  const { t: tQ } = useT("questions");
  const wsPaths = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const pending = useAgentPendingQuestions(agent.id);
  const { data: issues = [] } = useQuery(issueListOptions(wsId));
  const issueById = new Map<string, Issue>();
  for (const i of issues as Issue[]) issueById.set(i.id, i);

  if (pending.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        {t(($) => $.tabs.questions_empty)}
      </p>
    );
  }

  return (
    <div className="space-y-2">
      {pending.map((q) => (
        <Row
          key={q.id}
          q={q}
          agent={agent}
          issue={q.issue_id ? issueById.get(q.issue_id) ?? null : null}
          openLabel={t(($) => $.tabs.questions_open)}
          tag={q.multi_select ? tQ(($) => $.tag_multi) : tQ(($) => $.tag_single)}
          questionsHref={`${wsPaths.questions()}?agentId=${encodeURIComponent(agent.id)}`}
          issueHrefBuilder={(id) => wsPaths.issueDetail(id)}
        />
      ))}
    </div>
  );
}

function Row({
  q,
  issue,
  openLabel,
  tag,
  questionsHref,
  issueHrefBuilder,
}: {
  q: AgentQuestion;
  agent: Agent;
  issue: Issue | null;
  openLabel: string;
  tag: string;
  questionsHref: string;
  issueHrefBuilder: (id: string) => string;
}) {
  return (
    <div className="group flex items-start gap-2 rounded-md border bg-card px-3 py-2 text-sm transition-colors hover:bg-accent/40">
      <HelpCircle className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
      <div className="flex-1 min-w-0">
        <AppLink href={questionsHref} className="block hover:underline">
          <div className="flex items-center gap-1.5">
            <span
              className={
                TYPE_TAG_CLASSES +
                " " +
                (q.multi_select
                  ? "bg-amber-500/15 text-amber-700 dark:text-amber-400"
                  : "bg-sky-500/15 text-sky-700 dark:text-sky-400")
              }
            >
              [{tag}]
            </span>
            <span className="truncate font-medium">{q.header}</span>
          </div>
        </AppLink>
        {issue && (
          <AppLink
            href={issueHrefBuilder(issue.id)}
            className="mt-1 inline-flex items-center gap-1 rounded-md bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground hover:bg-accent hover:text-foreground"
            onClick={(e) => e.stopPropagation()}
          >
            <span className="font-mono">{issue.identifier}</span>
            <span className="line-clamp-1 max-w-[200px]">{issue.title}</span>
          </AppLink>
        )}
      </div>
      <AppLink
        href={questionsHref}
        className="shrink-0 self-start pt-0.5 text-xs text-muted-foreground group-hover:text-foreground"
      >
        {openLabel} →
      </AppLink>
    </div>
  );
}
