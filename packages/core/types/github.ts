export type PullRequestSource = "github" | "aone";

// "unknown" covers the manually-linked-but-not-yet-enriched case (mostly
// aone rows linked before `a1` could fetch their status). The frontend
// renders "unknown" with a generic icon and no badge color.
export type PullRequestState =
  | "open"
  | "closed"
  | "merged"
  | "draft"
  | "unknown";

export interface GitHubInstallation {
  id: string;
  workspace_id: string;
  installation_id: number;
  account_login: string;
  account_type: "User" | "Organization";
  account_avatar_url: string | null;
  created_at: string;
}

export interface PullRequest {
  id: string;
  workspace_id: string;
  source: PullRequestSource;
  // Nullable because aone rows may be saved before a1 enrichment populates
  // the parsed repo. github rows always have these set.
  repo_owner: string | null;
  repo_name: string | null;
  number: number | null;
  title: string;
  state: PullRequestState;
  html_url: string;
  branch: string | null;
  author_login: string | null;
  author_avatar_url: string | null;
  merged_at: string | null;
  closed_at: string | null;
  pr_created_at: string | null;
  pr_updated_at: string | null;
}

export interface ListGitHubInstallationsResponse {
  installations: GitHubInstallation[];
  /** Whether the deployment has GitHub App credentials configured. When false, the Connect button is hidden / disabled. */
  configured: boolean;
}

export interface GitHubConnectResponse {
  /** The GitHub App install URL the browser should open. Empty when `configured` is false. */
  url?: string;
  configured: boolean;
}
