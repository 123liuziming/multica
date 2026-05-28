"use client";

import { useEffect, useState } from "react";
import { Save } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { notifySummaryOptions } from "@multica/core/notify-summary/queries";
import { useUpdateNotifySummarySettings } from "@multica/core/notify-summary/mutations";
import type { NotifySummarySettings } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { toast } from "sonner";
import { useT } from "../../i18n";

const DEFAULTS: NotifySummarySettings = {
  enabled: false,
  template: "",
  idle_wait_secs: 10,
  max_wait_secs: 30,
  summary_length: 300,
};

export function NotifySummaryTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data, isLoading } = useQuery(notifySummaryOptions(wsId));
  const mutation = useUpdateNotifySummarySettings();

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";

  const [draft, setDraft] = useState<NotifySummarySettings>(DEFAULTS);

  // Re-sync local form state when the server payload arrives or changes
  // out-of-band (another admin's update, refetch after rollback).
  useEffect(() => {
    if (data?.settings) {
      setDraft(data.settings);
    }
  }, [data]);

  const handleSave = async () => {
    try {
      await mutation.mutateAsync(draft);
      toast.success(t(($) => $.notify_summary.toast_saved));
    } catch (e) {
      // Server returns the exact template-parse error so operators can find
      // their typo (Go text/template line numbers + the PascalCase hint).
      toast.error(e instanceof Error ? e.message : t(($) => $.notify_summary.toast_save_failed));
    }
  };

  // Template + throttle fields are meaningless when the feature is off — lock
  // them out (but still visible) so operators see the shape they're toggling.
  const fieldsLocked = !canManage || !draft.enabled;

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div>
          <h2 className="text-sm font-semibold">{t(($) => $.notify_summary.title)}</h2>
          <p className="text-sm text-muted-foreground mt-1">
            {t(($) => $.notify_summary.description)}
          </p>
        </div>

        <Card>
          <CardContent className="space-y-6">
            {/* Enable toggle */}
            <div className="flex items-center justify-between">
              <div className="space-y-0.5 pr-4">
                <p className="text-sm font-medium">{t(($) => $.notify_summary.enabled_label)}</p>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.notify_summary.enabled_hint)}
                </p>
              </div>
              <Switch
                checked={draft.enabled}
                disabled={!canManage || isLoading}
                onCheckedChange={(checked) => setDraft((d) => ({ ...d, enabled: checked }))}
              />
            </div>

            {/* Throttle row */}
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="notify-summary-idle">
                  {t(($) => $.notify_summary.idle_wait_label)}
                </Label>
                <Input
                  id="notify-summary-idle"
                  type="number"
                  min={1}
                  max={3600}
                  disabled={fieldsLocked}
                  value={draft.idle_wait_secs}
                  onChange={(e) => setDraft((d) => ({ ...d, idle_wait_secs: numberOr(e.target.value, d.idle_wait_secs) }))}
                />
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.notify_summary.idle_wait_hint)}
                </p>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="notify-summary-max">
                  {t(($) => $.notify_summary.max_wait_label)}
                </Label>
                <Input
                  id="notify-summary-max"
                  type="number"
                  min={1}
                  max={3600}
                  disabled={fieldsLocked}
                  value={draft.max_wait_secs}
                  onChange={(e) => setDraft((d) => ({ ...d, max_wait_secs: numberOr(e.target.value, d.max_wait_secs) }))}
                />
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.notify_summary.max_wait_hint)}
                </p>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="notify-summary-length">
                  {t(($) => $.notify_summary.summary_length_label)}
                </Label>
                <Input
                  id="notify-summary-length"
                  type="number"
                  min={50}
                  max={2000}
                  disabled={fieldsLocked}
                  value={draft.summary_length}
                  onChange={(e) => setDraft((d) => ({ ...d, summary_length: numberOr(e.target.value, d.summary_length) }))}
                />
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.notify_summary.summary_length_hint)}
                </p>
              </div>
            </div>

            {/* Template */}
            <div className="space-y-1.5">
              <Label htmlFor="notify-summary-template">
                {t(($) => $.notify_summary.template_label)}
              </Label>
              <Textarea
                id="notify-summary-template"
                rows={14}
                disabled={fieldsLocked}
                placeholder={t(($) => $.notify_summary.template_placeholder)}
                value={draft.template}
                onChange={(e) => setDraft((d) => ({ ...d, template: e.target.value }))}
                className="font-mono text-xs"
              />
              <p className="text-xs text-muted-foreground">
                {t(($) => $.notify_summary.template_hint)}
              </p>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.notify_summary.template_funcs_hint)}
              </p>
            </div>

            {/* Variable cheatsheet */}
            <details className="text-xs text-muted-foreground">
              <summary className="cursor-pointer select-none font-medium text-foreground">
                {t(($) => $.notify_summary.variables_title)}
              </summary>
              <div className="mt-2 grid grid-cols-1 sm:grid-cols-2 gap-x-4 gap-y-1 font-mono">
                {VARIABLE_HINTS.map(({ name, desc }) => (
                  <div key={name} className="flex">
                    <code className="text-foreground/80 mr-2">{`{{.${name}}}`}</code>
                    <span className="text-muted-foreground">{desc}</span>
                  </div>
                ))}
              </div>
            </details>

            <div className="flex justify-end pt-2">
              <Button onClick={handleSave} disabled={!canManage || mutation.isPending}>
                <Save className="h-4 w-4" />
                {mutation.isPending
                  ? t(($) => $.notify_summary.saving)
                  : t(($) => $.notify_summary.save)}
              </Button>
            </div>
          </CardContent>
        </Card>

        {!canManage && (
          <p className="text-xs text-muted-foreground">
            {t(($) => $.notify_summary.admin_only_hint)}
          </p>
        )}
      </section>
    </div>
  );
}

function numberOr(raw: string, fallback: number): number {
  const n = Number(raw);
  return Number.isFinite(n) && n >= 0 ? Math.floor(n) : fallback;
}

const VARIABLE_HINTS: { name: string; desc: string }[] = [
  { name: "IssueIdentifier", desc: "PREFIX-NN (e.g. ACME-101)" },
  { name: "IssueURL", desc: "Multica workspace URL" },
  { name: "AoneIssueURL", desc: "Aone work item URL (from [AONE-ID] + aone_project_id)" },
  { name: "IssueTitle", desc: "Issue title" },
  { name: "IssueStatus", desc: "DB status (todo/in_progress/…)" },
  { name: "IssueStatusTag", desc: "First label with status: prefix" },
  { name: "IssueStatusTagShort", desc: "Same, prefix stripped" },
  { name: "IssuePullRequests", desc: "Linked PR/MR URLs, one per line" },
  { name: "IssueCreateTime", desc: "RFC3339 timestamp" },
  { name: "IssueElapsed", desc: "Humanized elapsed (Chinese)" },
  { name: "SummaryLength", desc: "Target Chinese-char count" },
  { name: "NotificationCount", desc: "How many events batched" },
  { name: "CombinedContent", desc: "Concatenated raw bodies" },
  { name: "StaffID", desc: "Recipient DingTalk staff ID" },
  { name: "InboxType", desc: "issue_assigned / status_changed / …" },
];
