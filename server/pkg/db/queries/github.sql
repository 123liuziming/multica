-- =====================
-- GitHub Installation
-- =====================

-- name: ListGitHubInstallationsByWorkspace :many
SELECT * FROM github_installation
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetGitHubInstallationByInstallationID :one
SELECT * FROM github_installation
WHERE installation_id = $1;

-- name: GetGitHubInstallationByID :one
SELECT * FROM github_installation
WHERE id = $1;

-- name: CreateGitHubInstallation :one
INSERT INTO github_installation (
    workspace_id, installation_id, account_login, account_type, account_avatar_url, connected_by_id
) VALUES (
    $1, $2, $3, $4, sqlc.narg('account_avatar_url'), sqlc.narg('connected_by_id')
)
ON CONFLICT (installation_id) DO UPDATE SET
    workspace_id = EXCLUDED.workspace_id,
    account_login = EXCLUDED.account_login,
    account_type = EXCLUDED.account_type,
    account_avatar_url = EXCLUDED.account_avatar_url,
    connected_by_id = EXCLUDED.connected_by_id,
    updated_at = now()
RETURNING *;

-- name: DeleteGitHubInstallation :exec
DELETE FROM github_installation WHERE id = $1 AND workspace_id = $2;

-- name: DeleteGitHubInstallationByInstallationID :one
DELETE FROM github_installation WHERE installation_id = $1
RETURNING id, workspace_id;

-- =====================
-- GitHub Pull Request
-- =====================

-- name: UpsertGitHubPullRequest :one
INSERT INTO github_pull_request (
    workspace_id, installation_id, repo_owner, repo_name, pr_number,
    title, state, html_url, branch, author_login, author_avatar_url,
    merged_at, closed_at, pr_created_at, pr_updated_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, sqlc.narg('branch'), sqlc.narg('author_login'), sqlc.narg('author_avatar_url'),
    sqlc.narg('merged_at'), sqlc.narg('closed_at'), $9, $10
)
ON CONFLICT (workspace_id, repo_owner, repo_name, pr_number) DO UPDATE SET
    installation_id = EXCLUDED.installation_id,
    title = EXCLUDED.title,
    state = EXCLUDED.state,
    html_url = EXCLUDED.html_url,
    branch = EXCLUDED.branch,
    author_login = EXCLUDED.author_login,
    author_avatar_url = EXCLUDED.author_avatar_url,
    merged_at = EXCLUDED.merged_at,
    closed_at = EXCLUDED.closed_at,
    pr_updated_at = EXCLUDED.pr_updated_at,
    updated_at = now()
RETURNING *;

-- name: GetGitHubPullRequest :one
SELECT * FROM github_pull_request
WHERE workspace_id = $1 AND repo_owner = $2 AND repo_name = $3 AND pr_number = $4;

-- name: GetGitHubPullRequestByID :one
-- Workspace-scoped lookup used by the unlink handler so a guessed PR UUID
-- from another tenant returns 404 instead of unlinking unrelated data.
SELECT * FROM github_pull_request
WHERE id = $1 AND workspace_id = $2;

-- name: ListPullRequestsByIssue :many
-- Returns both github and aone rows for an issue. The aone branch is
-- placed FIRST in the UNION so sqlc infers nullable types for repo_owner /
-- repo_name / pr_number / pr_created_at / pr_updated_at (the github table
-- declares them NOT NULL, but aone allows them to be empty for arbitrary
-- URLs that don't parse cleanly).
SELECT
    apr.id,
    apr.workspace_id,
    'aone'::text AS source,
    apr.html_url,
    apr.title,
    apr.state,
    apr.repo_owner,
    apr.repo_name,
    apr.pr_number,
    NULL::text AS branch,
    apr.author_login,
    apr.author_avatar_url,
    apr.merged_at,
    apr.closed_at,
    apr.pr_created_at,
    apr.pr_updated_at
FROM aone_pull_request apr
JOIN issue_pull_request ipr ON ipr.pull_request_id = apr.id AND ipr.source = 'aone'
WHERE ipr.issue_id = $1
UNION ALL
SELECT
    gpr.id,
    gpr.workspace_id,
    'github'::text AS source,
    gpr.html_url,
    gpr.title,
    gpr.state,
    gpr.repo_owner,
    gpr.repo_name,
    gpr.pr_number,
    gpr.branch,
    gpr.author_login,
    gpr.author_avatar_url,
    gpr.merged_at,
    gpr.closed_at,
    gpr.pr_created_at,
    gpr.pr_updated_at
FROM github_pull_request gpr
JOIN issue_pull_request ipr ON ipr.pull_request_id = gpr.id AND ipr.source = 'github'
WHERE ipr.issue_id = $1
ORDER BY pr_created_at DESC NULLS LAST;

-- name: ListIssueIDsForPullRequest :many
SELECT issue_id FROM issue_pull_request
WHERE pull_request_id = $1;

-- name: GetSiblingPullRequestStateCountsForIssue :one
-- Returns, for the PRs linked to an issue excluding one PR by id (the PR
-- currently being processed by the webhook handler), how many are still in
-- flight (open or draft) and how many have already merged. The webhook
-- handler combines these with the current event's state to decide whether
-- to auto-advance the issue: the issue moves to done only when there is no
-- in-flight sibling AND at least one linked PR (current or sibling) merged.
SELECT
    COALESCE(SUM(CASE WHEN pr.state IN ('open', 'draft') THEN 1 ELSE 0 END), 0)::bigint AS open_count,
    COALESCE(SUM(CASE WHEN pr.state = 'merged' THEN 1 ELSE 0 END), 0)::bigint AS merged_count
FROM github_pull_request pr
JOIN issue_pull_request ipr ON ipr.pull_request_id = pr.id AND ipr.source = 'github'
WHERE ipr.issue_id = $1
  AND pr.id <> $2;

-- =====================
-- Issue ↔ Pull Request link
-- =====================

-- name: LinkIssueToPullRequest :exec
INSERT INTO issue_pull_request (
    issue_id, pull_request_id, source, linked_by_type, linked_by_id
) VALUES (
    $1, $2, $3, sqlc.narg('linked_by_type'), sqlc.narg('linked_by_id')
)
ON CONFLICT (issue_id, pull_request_id, source) DO NOTHING;

-- name: UnlinkIssueFromPullRequest :exec
DELETE FROM issue_pull_request
WHERE issue_id = $1 AND pull_request_id = $2 AND source = $3;
