CREATE UNIQUE INDEX IF NOT EXISTS idx_issue_origin_unique
    ON issue (workspace_id, origin_type, origin_id)
    WHERE origin_type IN ('quick_create', 'aone') AND origin_id IS NOT NULL;
