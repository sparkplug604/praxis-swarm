-- +goose Up

CREATE TABLE swarm_budgets (
    bucket TEXT PRIMARY KEY,
    token_limit INTEGER NOT NULL CHECK(token_limit > 0),
    reserved INTEGER NOT NULL DEFAULT 0 CHECK(reserved >= 0),
    spent INTEGER NOT NULL DEFAULT 0 CHECK(spent >= 0)
);
CREATE TABLE swarm_runs (
    id TEXT PRIMARY KEY,
    request_id TEXT NOT NULL UNIQUE,
    request_hash TEXT NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id),
    version INTEGER NOT NULL CHECK(version > 0),
    plan_json TEXT NOT NULL,
    cancelled BOOLEAN NOT NULL DEFAULT FALSE,
    integration_head TEXT NOT NULL,
    created_ns INTEGER NOT NULL
);
CREATE TABLE swarm_contracts (
    run_id TEXT NOT NULL REFERENCES swarm_runs(id),
    task_id TEXT NOT NULL,
    version INTEGER NOT NULL CHECK(version > 0),
    contract_hash TEXT NOT NULL,
    contract_json TEXT NOT NULL,
    PRIMARY KEY(run_id, task_id, version)
);
CREATE TABLE swarm_tasks (
    run_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    version INTEGER NOT NULL,
    accepted_attempt TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(run_id, task_id),
    FOREIGN KEY(run_id, task_id, version) REFERENCES swarm_contracts(run_id, task_id, version)
);
CREATE TABLE swarm_attempts (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    version INTEGER NOT NULL,
    request_id TEXT NOT NULL UNIQUE,
    request_hash TEXT NOT NULL,
    operation_id TEXT NOT NULL UNIQUE,
    spec_json TEXT NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('reserved','dispatching','running','submitted','accepted','failed','cancelled')),
    owner TEXT NOT NULL,
    generation INTEGER NOT NULL CHECK(generation > 0),
    lease_ns INTEGER NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    reported_model TEXT NOT NULL DEFAULT '',
    submission_commit TEXT NOT NULL DEFAULT '',
    created_ns INTEGER NOT NULL,
    FOREIGN KEY(run_id, task_id, version) REFERENCES swarm_contracts(run_id, task_id, version)
);
CREATE UNIQUE INDEX swarm_one_open_attempt ON swarm_attempts(run_id,task_id)
    WHERE state IN ('reserved','dispatching','running','submitted');
CREATE UNIQUE INDEX swarm_one_runtime_session ON swarm_attempts(session_id)
    WHERE session_id <> '';
CREATE TABLE swarm_resources (
    attempt_id TEXT NOT NULL REFERENCES swarm_attempts(id),
    namespace TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('path','semantic')),
    resource_key TEXT NOT NULL,
    PRIMARY KEY(namespace,kind,resource_key)
);
CREATE TABLE swarm_usage_reservations (
    attempt_id TEXT PRIMARY KEY REFERENCES swarm_attempts(id),
    bucket TEXT NOT NULL REFERENCES swarm_budgets(bucket),
    reserved INTEGER NOT NULL CHECK(reserved > 0),
    actual INTEGER NOT NULL DEFAULT -1 CHECK(actual >= -1),
    receipt_id TEXT UNIQUE
);
CREATE TABLE swarm_evidence (
    id TEXT PRIMARY KEY,
    attempt_id TEXT NOT NULL REFERENCES swarm_attempts(id),
    record_json TEXT NOT NULL
);
CREATE TABLE swarm_integrations (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES swarm_runs(id),
    record_json TEXT NOT NULL
);

-- Preserve contracts, attempt identity, and evidence even after cancellation.
-- +goose StatementBegin
CREATE TRIGGER swarm_contracts_immutable BEFORE UPDATE ON swarm_contracts
BEGIN SELECT RAISE(ABORT, 'swarm contracts are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER swarm_attempt_identity_immutable BEFORE UPDATE OF id,run_id,task_id,version,request_id,request_hash,operation_id,spec_json,created_ns ON swarm_attempts
BEGIN SELECT RAISE(ABORT, 'swarm attempt identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER swarm_evidence_immutable BEFORE UPDATE ON swarm_evidence
BEGIN SELECT RAISE(ABORT, 'swarm evidence is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER swarm_integrations_immutable BEFORE UPDATE ON swarm_integrations
BEGIN SELECT RAISE(ABORT, 'swarm integrations are immutable'); END;
-- +goose StatementEnd

-- +goose Down
DROP TABLE swarm_integrations;
DROP TABLE swarm_evidence;
DROP TABLE swarm_usage_reservations;
DROP TABLE swarm_resources;
DROP TABLE swarm_attempts;
DROP TABLE swarm_tasks;
DROP TABLE swarm_contracts;
DROP TABLE swarm_runs;
DROP TABLE swarm_budgets;
