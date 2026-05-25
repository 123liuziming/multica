-- Reverse of 094_aone_pull_request.up.sql.
--
-- Restoring the FK only succeeds when no rows reference an aone_pull_request
-- id. The migration runner cleans up Aone rows first, then re-anchors the
-- join table to github_pull_request.

DELETE FROM issue_pull_request WHERE source = 'aone';

ALTER TABLE issue_pull_request DROP CONSTRAINT issue_pull_request_pkey;
ALTER TABLE issue_pull_request ADD PRIMARY KEY (issue_id, pull_request_id);
ALTER TABLE issue_pull_request DROP COLUMN source;

ALTER TABLE issue_pull_request
    ADD CONSTRAINT issue_pull_request_pull_request_id_fkey
    FOREIGN KEY (pull_request_id) REFERENCES github_pull_request(id) ON DELETE CASCADE;

DROP INDEX IF EXISTS idx_aone_pull_request_workspace;
DROP TABLE aone_pull_request;
