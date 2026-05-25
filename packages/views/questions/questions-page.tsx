"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  workspaceQuestionsOptions,
  questionCountsOptions,
} from "@multica/core/questions";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { AgentQuestion, Agent, Issue } from "@multica/core/types";
import { useNavigation, AppLink } from "../navigation";
import { useT } from "../i18n";
import { PageHeader } from "../layout/page-header";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@multica/ui/components/ui/resizable";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { ChevronDown, ChevronRight, FileText, HelpCircle, Inbox } from "lucide-react";
import { ActorAvatar } from "../common/actor-avatar";
import { cn } from "@multica/ui/lib/utils";
import { useDefaultLayout } from "react-resizable-panels";
import { QuestionCard } from "./components/question-card";

interface IssueGroup {
  issueId: string;
  questions: AgentQuestion[];
  pendingCount: number;
  resolvedCount: number;
}
interface AgentGroup {
  agentId: string;
  questions: AgentQuestion[];
  pendingCount: number;
  resolvedCount: number;
}

type Selection =
  | { kind: "issue"; issueId: string }
  | { kind: "agent"; agentId: string }
  | null;

function buildIssueGroups(qs: AgentQuestion[]): IssueGroup[] {
  const map = new Map<string, IssueGroup>();
  for (const q of qs) {
    if (!q.issue_id) continue;
    let g = map.get(q.issue_id);
    if (!g) {
      g = { issueId: q.issue_id, questions: [], pendingCount: 0, resolvedCount: 0 };
      map.set(q.issue_id, g);
    }
    g.questions.push(q);
    if (q.status === "pending") g.pendingCount += 1;
    else g.resolvedCount += 1;
  }
  // Pending first, then by recency.
  return Array.from(map.values()).sort((a, b) => {
    if (a.pendingCount !== b.pendingCount) return b.pendingCount - a.pendingCount;
    const aLatest = a.questions[0]?.created_at ?? "";
    const bLatest = b.questions[0]?.created_at ?? "";
    return bLatest.localeCompare(aLatest);
  });
}

function buildAgentGroups(qs: AgentQuestion[]): AgentGroup[] {
  const map = new Map<string, AgentGroup>();
  for (const q of qs) {
    let g = map.get(q.agent_id);
    if (!g) {
      g = { agentId: q.agent_id, questions: [], pendingCount: 0, resolvedCount: 0 };
      map.set(q.agent_id, g);
    }
    g.questions.push(q);
    if (q.status === "pending") g.pendingCount += 1;
    else g.resolvedCount += 1;
  }
  return Array.from(map.values()).sort((a, b) => {
    if (a.pendingCount !== b.pendingCount) return b.pendingCount - a.pendingCount;
    const aLatest = a.questions[0]?.created_at ?? "";
    const bLatest = b.questions[0]?.created_at ?? "";
    return bLatest.localeCompare(aLatest);
  });
}

export function QuestionsPage() {
  const { t } = useT("questions");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { searchParams, replace } = useNavigation();
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_questions_layout",
  });

  const allQuestionsQuery = useQuery(workspaceQuestionsOptions(wsId, "all"));
  const allQuestions: AgentQuestion[] = allQuestionsQuery.data ?? [];
  const isLoading = allQuestionsQuery.isLoading;
  useQuery(questionCountsOptions(wsId));

  const issueGroups = useMemo(() => buildIssueGroups(allQuestions), [allQuestions]);
  const pendingIssueGroups = useMemo(
    () => issueGroups.filter((g) => g.resolvedCount !== g.questions.length),
    [issueGroups],
  );
  const answeredIssueGroups = useMemo(
    () => issueGroups.filter((g) => g.resolvedCount === g.questions.length),
    [issueGroups],
  );
  const agentGroups = useMemo(() => buildAgentGroups(allQuestions), [allQuestions]);

  const urlIssueId = searchParams.get("issueId") ?? "";
  const urlAgentId = searchParams.get("agentId") ?? "";

  const selection: Selection = useMemo(() => {
    if (urlIssueId && issueGroups.some((g) => g.issueId === urlIssueId)) {
      return { kind: "issue", issueId: urlIssueId };
    }
    if (urlAgentId && agentGroups.some((g) => g.agentId === urlAgentId)) {
      return { kind: "agent", agentId: urlAgentId };
    }
    if (pendingIssueGroups[0]) return { kind: "issue", issueId: pendingIssueGroups[0].issueId };
    if (agentGroups[0]) return { kind: "agent", agentId: agentGroups[0].agentId };
    if (answeredIssueGroups[0]) return { kind: "issue", issueId: answeredIssueGroups[0].issueId };
    return null;
  }, [urlIssueId, urlAgentId, issueGroups, pendingIssueGroups, answeredIssueGroups, agentGroups]);

  const select = (next: Selection) => {
    const params = new URLSearchParams();
    if (next?.kind === "issue") params.set("issueId", next.issueId);
    if (next?.kind === "agent") params.set("agentId", next.agentId);
    replace(`${wsPaths.questions()}${params.toString() ? `?${params.toString()}` : ""}`);
  };

  // Sync first-load selection into the URL so deep links keep working.
  useEffect(() => {
    if (!selection) return;
    if (selection.kind === "issue" && urlIssueId !== selection.issueId) {
      const p = new URLSearchParams();
      p.set("issueId", selection.issueId);
      replace(`${wsPaths.questions()}?${p.toString()}`);
    } else if (selection.kind === "agent" && urlAgentId !== selection.agentId) {
      const p = new URLSearchParams();
      p.set("agentId", selection.agentId);
      replace(`${wsPaths.questions()}?${p.toString()}`);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selection?.kind, selection?.kind === "issue" ? selection.issueId : selection?.agentId]);

  return (
    <div className="flex h-full flex-col">
      <PageHeader>
        <h1 className="text-sm font-semibold">{t(($) => $.page_title)}</h1>
      </PageHeader>
      <div className="flex-1 min-h-0">
        {isLoading ? (
          <LoadingState />
        ) : issueGroups.length === 0 && agentGroups.length === 0 ? (
          <EmptyState text={t(($) => $.empty_state)} />
        ) : (
          <ResizablePanelGroup
            orientation="horizontal"
            className="flex-1 h-full min-h-0"
            defaultLayout={defaultLayout}
            onLayoutChanged={onLayoutChanged}
          >
            <ResizablePanel id="list" defaultSize={320} minSize={240} maxSize={480} groupResizeBehavior="preserve-pixel-size">
              <LeftSidebar
                pendingIssueGroups={pendingIssueGroups}
                agentGroups={agentGroups}
                answeredIssueGroups={answeredIssueGroups}
                selection={selection}
                onSelect={select}
              />
            </ResizablePanel>
            <ResizableHandle />
            <ResizablePanel id="detail" minSize="40%">
              <RightPane wsId={wsId} selection={selection} issueGroups={issueGroups} agentGroups={agentGroups} />
            </ResizablePanel>
          </ResizablePanelGroup>
        )}
      </div>
    </div>
  );
}

function LeftSidebar({
  pendingIssueGroups,
  agentGroups,
  answeredIssueGroups,
  selection,
  onSelect,
}: {
  pendingIssueGroups: IssueGroup[];
  agentGroups: AgentGroup[];
  answeredIssueGroups: IssueGroup[];
  selection: Selection;
  onSelect: (s: Selection) => void;
}) {
  const { t } = useT("questions");
  const [pendingIssuesOpen, setPendingIssuesOpen] = useState(true);
  const [agentsOpen, setAgentsOpen] = useState(false);
  const [answeredIssuesOpen, setAnsweredIssuesOpen] = useState(false);

  return (
    <div className="flex h-full flex-col border-r">
      <div className="flex-1 min-h-0 overflow-y-auto py-2">
        <SectionHeader
          label={t(($) => $.left_section_pending_issue_questions)}
          count={pendingIssueGroups.length}
          open={pendingIssuesOpen}
          onToggle={() => setPendingIssuesOpen((v) => !v)}
        />
        {pendingIssuesOpen && (
          <div className="pb-3">
            {pendingIssueGroups.length === 0 ? (
              <p className="px-4 py-3 text-xs text-muted-foreground">
                {t(($) => $.left_no_issue_questions)}
              </p>
            ) : (
              pendingIssueGroups.map((g) => (
                <IssueRow
                  key={g.issueId}
                  group={g}
                  selected={selection?.kind === "issue" && selection.issueId === g.issueId}
                  onSelect={() => onSelect({ kind: "issue", issueId: g.issueId })}
                />
              ))
            )}
          </div>
        )}

        <SectionHeader
          label={t(($) => $.left_section_agent_questions)}
          count={agentGroups.length}
          open={agentsOpen}
          onToggle={() => setAgentsOpen((v) => !v)}
        />
        {agentsOpen && (
          <div className="pb-3">
            {agentGroups.length === 0 ? (
              <p className="px-4 py-3 text-xs text-muted-foreground">
                {t(($) => $.left_no_agent_questions)}
              </p>
            ) : (
              agentGroups.map((g) => (
                <AgentRow
                  key={g.agentId}
                  group={g}
                  selected={selection?.kind === "agent" && selection.agentId === g.agentId}
                  onSelect={() => onSelect({ kind: "agent", agentId: g.agentId })}
                />
              ))
            )}
          </div>
        )}

        <SectionHeader
          label={t(($) => $.left_section_answered_issue_questions)}
          count={answeredIssueGroups.length}
          open={answeredIssuesOpen}
          onToggle={() => setAnsweredIssuesOpen((v) => !v)}
        />
        {answeredIssuesOpen && (
          <div className="pb-3">
            {answeredIssueGroups.length === 0 ? (
              <p className="px-4 py-3 text-xs text-muted-foreground">
                {t(($) => $.left_no_answered_issue_questions)}
              </p>
            ) : (
              answeredIssueGroups.map((g) => (
                <IssueRow
                  key={g.issueId}
                  group={g}
                  selected={selection?.kind === "issue" && selection.issueId === g.issueId}
                  onSelect={() => onSelect({ kind: "issue", issueId: g.issueId })}
                />
              ))
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function SectionHeader({
  label,
  count,
  open,
  onToggle,
}: {
  label: string;
  count: number;
  open: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onToggle}
      className="flex w-full items-center gap-2 border-b border-transparent px-3 py-2 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground hover:bg-accent/30"
    >
      {open ? (
        <ChevronDown className="h-3.5 w-3.5" />
      ) : (
        <ChevronRight className="h-3.5 w-3.5" />
      )}
      <span>{label}</span>
      <span className="ml-auto rounded-full bg-muted px-1.5 text-[10px] font-medium">
        {count}
      </span>
    </button>
  );
}

function IssueRow({
  group,
  selected,
  onSelect,
}: {
  group: IssueGroup;
  selected: boolean;
  onSelect: () => void;
}) {
  const { t } = useT("questions");
  const wsId = useWorkspaceId();
  const issueQuery = useQuery({
    ...issueDetailOptions(wsId, group.issueId),
    enabled: !!wsId && !!group.issueId,
  });
  const issue = issueQuery.data;
  const title = issue
    ? `${issue.identifier} ${issue.title}`
    : group.issueId.slice(0, 8);
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "flex w-full flex-col gap-1 px-4 py-2 text-left text-sm transition-colors hover:bg-accent/40",
        selected && "bg-accent",
      )}
    >
      <span className="truncate font-medium">{title}</span>
      <span className="flex items-center gap-3 text-xs text-muted-foreground">
        {group.pendingCount > 0 && (
          <span className="flex items-center gap-1">
            <HelpCircle className="h-3 w-3" />
            {t(($) => $.list_pending_count, { count: group.pendingCount })}
          </span>
        )}
        {group.resolvedCount > 0 && (
          <span>{t(($) => $.list_resolved_count, { count: group.resolvedCount })}</span>
        )}
      </span>
    </button>
  );
}

function AgentRow({
  group,
  selected,
  onSelect,
}: {
  group: AgentGroup;
  selected: boolean;
  onSelect: () => void;
}) {
  const { t } = useT("questions");
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const agent = (agents as Agent[]).find((a) => a.id === group.agentId);
  const title = agent ? agent.name : group.agentId.slice(0, 8);
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "flex w-full flex-col gap-1 px-4 py-2 text-left text-sm transition-colors hover:bg-accent/40",
        selected && "bg-accent",
      )}
    >
      <span className="flex items-center gap-1.5 truncate font-medium">
        <ActorAvatar actorType="agent" actorId={group.agentId} size={16} />
        {title}
      </span>
      <span className="flex items-center gap-3 text-xs text-muted-foreground">
        {group.pendingCount > 0 && (
          <span className="flex items-center gap-1">
            <HelpCircle className="h-3 w-3" />
            {t(($) => $.list_pending_count, { count: group.pendingCount })}
          </span>
        )}
        {group.resolvedCount > 0 && (
          <span>{t(($) => $.list_resolved_count, { count: group.resolvedCount })}</span>
        )}
      </span>
    </button>
  );
}

function RightPane({
  wsId,
  selection,
  issueGroups,
  agentGroups,
}: {
  wsId: string;
  selection: Selection;
  issueGroups: IssueGroup[];
  agentGroups: AgentGroup[];
}) {
  const { t } = useT("questions");
  if (!selection) {
    return (
      <div className="flex h-full items-center justify-center p-6 text-sm text-muted-foreground">
        {t(($) => $.no_selection)}
      </div>
    );
  }
  if (selection.kind === "issue") {
    const group = issueGroups.find((g) => g.issueId === selection.issueId);
    if (!group) return null;
    return <IssueDetailPanel wsId={wsId} group={group} />;
  }
  const group = agentGroups.find((g) => g.agentId === selection.agentId);
  if (!group) return null;
  return <AgentDetailPanel wsId={wsId} group={group} />;
}

function IssueDetailPanel({ wsId, group }: { wsId: string; group: IssueGroup }) {
  const { t } = useT("questions");
  const wsPaths = useWorkspacePaths();
  const issueQuery = useQuery(issueDetailOptions(wsId, group.issueId));
  const issue: Issue | undefined = issueQuery.data;
  const pending = group.questions.filter((q) => q.status === "pending");
  const resolved = group.questions.filter((q) => q.status !== "pending");
  const href = wsPaths.issueDetail(group.issueId);
  return (
    <div className="flex h-full flex-col">
      <div className="border-b px-6 py-4">
        <AppLink
          href={href}
          className="group flex items-start gap-3 rounded-md -m-1 p-1 transition-colors hover:bg-accent/40"
        >
          <span className="mt-1 flex size-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
            <FileText className="h-4 w-4" />
          </span>
          <div className="flex-1 min-w-0">
            <div className="text-xs text-muted-foreground">
              {issue?.identifier ?? group.issueId.slice(0, 8)}
            </div>
            <h2 className="text-base font-semibold group-hover:underline">
              {issue?.title ?? "—"}
            </h2>
            {issue?.description ? (
              <p className="mt-0.5 line-clamp-2 text-sm text-muted-foreground whitespace-pre-wrap">
                {issue.description}
              </p>
            ) : (
              <p className="mt-0.5 text-xs italic text-muted-foreground">
                {t(($) => $.right_no_description)}
              </p>
            )}
          </div>
          <span className="shrink-0 self-center text-xs text-muted-foreground group-hover:text-foreground">
            {t(($) => $.right_open_issue)} →
          </span>
        </AppLink>
      </div>
      <QuestionsList pending={pending} resolved={resolved} resetKey={group.issueId} />
    </div>
  );
}

function AgentDetailPanel({ wsId, group }: { wsId: string; group: AgentGroup }) {
  const { t } = useT("questions");
  const wsPaths = useWorkspacePaths();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const agent = (agents as Agent[]).find((a) => a.id === group.agentId);
  const pending = group.questions.filter((q) => q.status === "pending");
  const resolved = group.questions.filter((q) => q.status !== "pending");
  const href = wsPaths.agentDetail(group.agentId);
  return (
    <div className="flex h-full flex-col">
      <div className="border-b px-6 py-4">
        <AppLink
          href={href}
          className="group flex items-start gap-3 rounded-md -m-1 p-1 transition-colors hover:bg-accent/40"
        >
          <ActorAvatar
            actorType="agent"
            actorId={group.agentId}
            size={36}
          />
          <div className="flex-1 min-w-0">
            <h2 className="text-base font-semibold group-hover:underline">
              {agent?.name ?? group.agentId.slice(0, 8)}
            </h2>
            {agent?.description ? (
              <p className="mt-0.5 line-clamp-2 text-sm text-muted-foreground whitespace-pre-wrap">
                {agent.description}
              </p>
            ) : (
              <p className="mt-0.5 text-xs italic text-muted-foreground">
                {t(($) => $.right_no_description)}
              </p>
            )}
          </div>
          <span className="shrink-0 self-center text-xs text-muted-foreground group-hover:text-foreground">
            {t(($) => $.right_open_agent)} →
          </span>
        </AppLink>
      </div>
      <QuestionsList
        pending={pending}
        resolved={resolved}
        resetKey={group.agentId}
        showIssueLink
      />
    </div>
  );
}

function QuestionsList({
  pending,
  resolved,
  resetKey,
  showIssueLink = false,
}: {
  pending: AgentQuestion[];
  resolved: AgentQuestion[];
  resetKey: string;
  showIssueLink?: boolean;
}) {
  const { t } = useT("questions");
  const defaultShowResolved = pending.length === 0 && resolved.length > 0;
  const [showResolved, setShowResolved] = useState(defaultShowResolved);

  useEffect(() => {
    setShowResolved(defaultShowResolved);
  }, [defaultShowResolved, resetKey]);

  return (
    <div className="flex-1 min-h-0 overflow-y-auto">
      <div className="space-y-3 p-6">
        <h3 className="text-sm font-medium text-foreground">
          {t(($) => $.pending_section_title)} ({pending.length})
        </h3>
        {pending.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            {t(($) => $.pending_section_empty)}
          </p>
        ) : (
          pending.map((q) => (
            <QuestionCard key={q.id} question={q} showIssueLink={showIssueLink} />
          ))
        )}
        <button
          type="button"
          onClick={() => setShowResolved((v) => !v)}
          className="flex items-center gap-1 pt-4 text-sm font-medium text-muted-foreground hover:text-foreground"
        >
          {showResolved ? (
            <ChevronDown className="h-4 w-4" />
          ) : (
            <ChevronRight className="h-4 w-4" />
          )}
          {t(($) => $.resolved_section_title)} ({resolved.length})
        </button>
        {showResolved && resolved.length === 0 && (
          <p className="pl-5 text-sm text-muted-foreground">
            {t(($) => $.resolved_section_empty)}
          </p>
        )}
        {showResolved &&
          resolved.map((q) => (
            <QuestionCard key={q.id} question={q} showIssueLink={showIssueLink} />
          ))}
      </div>
    </div>
  );
}

function LoadingState() {
  return (
    <div className="flex h-full flex-col gap-2 p-6">
      <Skeleton className="h-6 w-1/3" />
      <Skeleton className="h-32 w-full" />
      <Skeleton className="h-32 w-full" />
    </div>
  );
}

function EmptyState({ text }: { text: string }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-2 p-6 text-muted-foreground">
      <Inbox className="h-8 w-8" />
      <p className="text-sm">{text}</p>
    </div>
  );
}
