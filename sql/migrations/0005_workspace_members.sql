-- +goose Up
CREATE TABLE workspace_members (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    actor_type text NOT NULL,
    actor_id text NOT NULL,
    role text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, actor_type, actor_id),
    CHECK (actor_type IN ('human', 'agent', 'system')),
    CHECK (btrim(actor_id) <> ''),
    CHECK (role IN ('owner', 'admin', 'member', 'viewer'))
);

CREATE INDEX idx_workspace_members_actor
    ON workspace_members(actor_type, actor_id, workspace_id);

-- +goose Down
DROP TABLE IF EXISTS workspace_members;
