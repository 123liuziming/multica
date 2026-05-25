ALTER TABLE agent
    ADD COLUMN allow_ask_user_question BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE agent_question (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    task_id UUID NOT NULL REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    issue_id UUID REFERENCES issue(id) ON DELETE CASCADE,
    header TEXT NOT NULL,
    question TEXT NOT NULL,
    options JSONB NOT NULL DEFAULT '[]'::jsonb,
    multi_select BOOLEAN NOT NULL DEFAULT false,
    status TEXT NOT NULL DEFAULT 'pending',
    answer_option_indices JSONB,
    answer_custom_text TEXT,
    answered_by_user_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    answered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_agent_question_workspace_status
    ON agent_question (workspace_id, status, created_at DESC);
CREATE INDEX idx_agent_question_issue
    ON agent_question (issue_id, status, created_at DESC)
    WHERE issue_id IS NOT NULL;
CREATE INDEX idx_agent_question_agent
    ON agent_question (agent_id, status, created_at DESC);
CREATE INDEX idx_agent_question_task
    ON agent_question (task_id);
