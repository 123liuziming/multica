/**
 * Per-workspace notify-summary configuration. Controls the multica → cc-connect
 * /notify-session pipeline that batches inbox events per (staff, issue) and
 * injects a single rendered prompt into the user's 1:1 chat.
 *
 * Mirrors the Go `notifysummary.Settings` struct field-for-field.
 */
export interface NotifySummarySettings {
  /** When false (default) the existing direct push path runs unchanged. */
  enabled: boolean;
  /**
   * Go text/template body. Empty falls back to the server's built-in
   * markdown-card default. Template variables use PascalCase
   * ({{.IssueTitle}}, not {{.issueTitle}}) — server returns a 400 with the
   * exact hint on parse error.
   */
  template: string;
  /** Idle wait in seconds. The bucket flushes after this much quiet. */
  idle_wait_secs: number;
  /** Hard upper bound on a bucket's lifetime, regardless of idle resets. */
  max_wait_secs: number;
  /** Target Chinese-character count for the summary paragraph. */
  summary_length: number;
}

export interface NotifySummarySettingsResponse {
  workspace_id: string;
  settings: NotifySummarySettings;
}
