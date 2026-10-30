-- +goose Up
CREATE TABLE swarm_scheduler_policies (
    pool_id TEXT PRIMARY KEY,
    singleton INTEGER NOT NULL DEFAULT 1 UNIQUE CHECK(singleton = 1),
    policy_hash TEXT NOT NULL,
    policy_json TEXT NOT NULL
);
CREATE TABLE swarm_schedule_bindings (
    attempt_id TEXT PRIMARY KEY REFERENCES swarm_attempts(id),
    pool_id TEXT NOT NULL REFERENCES swarm_scheduler_policies(pool_id),
    provider_key TEXT NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id),
    workspace_root TEXT NOT NULL,
    slot_kind TEXT NOT NULL CHECK(slot_kind IN ('worker','reviewer'))
);
CREATE INDEX swarm_schedule_pool ON swarm_schedule_bindings(pool_id);
-- +goose StatementBegin
CREATE TRIGGER swarm_scheduler_policy_immutable BEFORE UPDATE ON swarm_scheduler_policies
BEGIN SELECT RAISE(ABORT, 'swarm scheduler policy is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER swarm_schedule_binding_immutable BEFORE UPDATE ON swarm_schedule_bindings
BEGIN SELECT RAISE(ABORT, 'swarm schedule binding is immutable'); END;
-- +goose StatementEnd

-- +goose Down
DROP TABLE swarm_schedule_bindings;
DROP TABLE swarm_scheduler_policies;
