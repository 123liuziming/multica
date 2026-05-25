-- name: CreateAgentQuestion :one
INSERT INTO agent_question (
    workspace_id, task_id, agent_id, issue_id,
    header, question, options, multi_select
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: FindMatchingIssueQuestion :one
SELECT * FROM agent_question
WHERE workspace_id = $1
  AND issue_id = $2
  AND header = $3
  AND question = $4
  AND options = @options::jsonb
  AND multi_select = @multi_select
  AND status IN ('pending', 'answered')
ORDER BY created_at DESC
LIMIT 1;

-- name: GetAgentQuestion :one
SELECT * FROM agent_question
WHERE id = $1;

-- name: GetAgentQuestionInWorkspace :one
SELECT * FROM agent_question
WHERE id = $1 AND workspace_id = $2;

-- name: AnswerAgentQuestion :one
UPDATE agent_question
SET status = 'answered',
    answer_option_indices = $2,
    answer_custom_text = $3,
    answered_by_user_id = $4,
    answered_at = now()
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: CancelAgentQuestion :one
UPDATE agent_question
SET status = 'cancelled', answered_at = now()
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: CancelPendingQuestionsForTask :many
UPDATE agent_question
SET status = 'cancelled', answered_at = now()
WHERE task_id = $1 AND status = 'pending'
RETURNING *;

-- name: DeletePendingQuestionsForTask :many
-- Used for mid-cancel paths: drop the row entirely instead of leaving a
-- "cancelled" tombstone. Daemon long-polls are still woken via an
-- in-memory cancelled payload (see deletePendingQuestionsForTask in the
-- handler) so there's no UX regression.
DELETE FROM agent_question
WHERE task_id = $1 AND status = 'pending'
RETURNING *;

-- name: ListWorkspaceQuestions :many
SELECT * FROM agent_question
WHERE workspace_id = $1
  AND (sqlc.narg('status_filter')::text IS NULL OR status = sqlc.narg('status_filter')::text)
ORDER BY created_at DESC
LIMIT $2;

-- name: ListIssueQuestions :many
SELECT * FROM agent_question
WHERE issue_id = $1
  AND (sqlc.narg('status_filter')::text IS NULL OR status = sqlc.narg('status_filter')::text)
ORDER BY created_at DESC;

-- name: ListAgentQuestions :many
SELECT * FROM agent_question
WHERE agent_id = $1
  AND (sqlc.narg('status_filter')::text IS NULL OR status = sqlc.narg('status_filter')::text)
ORDER BY created_at DESC
LIMIT $2;

-- name: ListTaskQuestions :many
SELECT * FROM agent_question
WHERE task_id = $1
ORDER BY created_at ASC;

-- name: CountPendingQuestionsByIssue :many
SELECT issue_id, COUNT(*)::int AS pending_count
FROM agent_question
WHERE workspace_id = $1
  AND status = 'pending'
  AND issue_id IS NOT NULL
GROUP BY issue_id;

-- name: CountWorkspacePendingQuestions :one
SELECT COUNT(*)::int FROM agent_question
WHERE workspace_id = $1 AND status = 'pending';
