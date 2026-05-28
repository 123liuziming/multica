import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const notifySummaryKeys = {
  all: (wsId: string) => ["notify-summary-settings", wsId] as const,
};

export function notifySummaryOptions(wsId: string) {
  return queryOptions({
    queryKey: notifySummaryKeys.all(wsId),
    queryFn: () => api.getNotifySummarySettings(wsId),
    enabled: !!wsId,
  });
}
