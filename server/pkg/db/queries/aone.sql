-- Aone (Alibaba code-review) pull requests, manually linked by users.
-- Schema mirrors github_pull_request where possible so the UNION-ed
-- ListPullRequestsByIssue query in github.sql produces uniform rows.

-- name: UpsertAonePullRequestByURL :one
-- Insert or update by normalized html_url. Title and state are written
-- from the caller's best guess (derived from URL, or enriched via `a1`).
-- last_enriched_at is only set when the caller actually obtained metadata
-- from a1 — otherwise the periodic refresh loop knows to retry.
INSERT INTO aone_pull_request (
    workspace_id, html_url, title, state,
    repo_owner, repo_name, pr_number,
    last_enriched_at
) VALUES (
    $1, $2, $3, $4,
    sqlc.narg('repo_owner'), sqlc.narg('repo_name'), sqlc.narg('pr_number'),
    sqlc.narg('last_enriched_at')
)
ON CONFLICT (workspace_id, html_url) DO UPDATE SET
    title = EXCLUDED.title,
    state = EXCLUDED.state,
    last_enriched_at = COALESCE(EXCLUDED.last_enriched_at, aone_pull_request.last_enriched_at),
    updated_at = now()
RETURNING *;

-- name: GetAonePullRequestByID :one
SELECT * FROM aone_pull_request
WHERE id = $1 AND workspace_id = $2;

-- name: ListAonePullRequestsForRefresh :many
-- Rows whose last enrichment is stale (or has never run). Ordered with
-- nulls first so the refresh loop catches new links immediately.
SELECT * FROM aone_pull_request
WHERE last_enriched_at IS NULL
   OR last_enriched_at < now() - make_interval(secs => sqlc.arg('stale_after_seconds')::int)
ORDER BY COALESCE(last_enriched_at, 'epoch'::timestamptz) ASC
LIMIT $1;

-- name: UpdateAonePullRequestEnrichment :exec
UPDATE aone_pull_request
SET title = $2,
    state = $3,
    last_enriched_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: TouchAonePullRequestEnrichment :exec
-- Bump last_enriched_at without changing title/state. The refresh loop
-- calls this when a1 returns identical metadata, so the row drops out of
-- the next ListAonePullRequestsForRefresh sweep until it goes stale again.
UPDATE aone_pull_request
SET last_enriched_at = now()
WHERE id = $1;
