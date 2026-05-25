-- Two-table model for manual PR linking: github_pull_request stays as the
-- webhook-fed source of truth for GitHub PRs; aone_pull_request is the new
-- store for Alibaba Aone code-review URLs. Other URL schemes are not
-- supported.
--
-- The join table issue_pull_request gains a `source` column so the same
-- pull_request_id UUID can be routed to either table without ambiguity.
-- The original FK on pull_request_id → github_pull_request is dropped
-- because the column is now polymorphic; no in-product code deletes PR
-- rows (mirrors DetachLabel), so app-level cleanup is not required.

CREATE TABLE aone_pull_request (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    html_url            TEXT NOT NULL,
    title               TEXT NOT NULL,
    state               TEXT NOT NULL DEFAULT 'unknown'
        CHECK (state IN ('open', 'closed', 'merged', 'draft', 'unknown')),
    repo_owner          TEXT,
    repo_name           TEXT,
    pr_number           INTEGER,
    author_login        TEXT,
    author_avatar_url   TEXT,
    merged_at           TIMESTAMPTZ,
    closed_at           TIMESTAMPTZ,
    pr_created_at       TIMESTAMPTZ,
    pr_updated_at       TIMESTAMPTZ,
    last_enriched_at    TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, html_url)
);

CREATE INDEX idx_aone_pull_request_workspace ON aone_pull_request(workspace_id);

ALTER TABLE issue_pull_request
    DROP CONSTRAINT issue_pull_request_pull_request_id_fkey;

ALTER TABLE issue_pull_request
    ADD COLUMN source TEXT NOT NULL DEFAULT 'github'
        CHECK (source IN ('github', 'aone'));

ALTER TABLE issue_pull_request DROP CONSTRAINT issue_pull_request_pkey;
ALTER TABLE issue_pull_request ADD PRIMARY KEY (issue_id, pull_request_id, source);
