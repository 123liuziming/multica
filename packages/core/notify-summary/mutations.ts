import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { notifySummaryKeys } from "./queries";
import type { NotifySummarySettings, NotifySummarySettingsResponse } from "../types";

/**
 * Updates the per-workspace notify-summary settings. Optimistic update on the
 * cache, rollback on error, invalidate on settle. Server validation errors
 * (e.g. malformed Go template) propagate via the mutation's onError so the
 * caller can surface the server-supplied message verbatim — operators rely
 * on the exact parse-error text to find their typo.
 */
export function useUpdateNotifySummarySettings() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (settings: NotifySummarySettings) =>
      api.updateNotifySummarySettings(wsId, settings),
    onMutate: async (settings) => {
      await qc.cancelQueries({ queryKey: notifySummaryKeys.all(wsId) });
      const prev = qc.getQueryData<NotifySummarySettingsResponse>(
        notifySummaryKeys.all(wsId),
      );
      qc.setQueryData<NotifySummarySettingsResponse>(
        notifySummaryKeys.all(wsId),
        (old) => (old ? { ...old, settings } : { workspace_id: wsId, settings }),
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(notifySummaryKeys.all(wsId), ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: notifySummaryKeys.all(wsId) });
    },
  });
}
