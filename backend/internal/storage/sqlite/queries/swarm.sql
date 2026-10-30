-- Acquire SQLite's writer reservation before any read, even on an empty store.
-- This is deliberately a no-op; per-Store mutexes alone do not fence processes.
-- name: LockSwarmWriter :exec
UPDATE swarm_runs SET version = version WHERE 0;

-- name: InsertSwarmBudget :exec
INSERT INTO swarm_budgets(bucket,token_limit) VALUES (?,?) ON CONFLICT(bucket) DO NOTHING;
-- name: GetSwarmBudget :one
SELECT * FROM swarm_budgets WHERE bucket = ?;
-- name: ReserveSwarmBudget :execrows
UPDATE swarm_budgets SET reserved = reserved + sqlc.arg(tokens)
WHERE bucket = sqlc.arg(bucket) AND sqlc.arg(tokens) <= token_limit - spent - reserved;
-- name: SettleSwarmBudget :exec
UPDATE swarm_budgets SET reserved = reserved - sqlc.arg(reserved_tokens), spent = spent + sqlc.arg(actual_tokens)
WHERE bucket = sqlc.arg(bucket);

-- name: InsertSwarmRun :exec
INSERT INTO swarm_runs(id,request_id,request_hash,project_id,version,plan_json,integration_head,created_ns)
VALUES (?,?,?,?,?,?,?,?);
-- name: GetSwarmRun :one
SELECT * FROM swarm_runs WHERE id = ?;
-- name: GetSwarmRunByRequest :one
SELECT * FROM swarm_runs WHERE request_id = ?;
-- name: ListSwarmRuns :many
SELECT * FROM swarm_runs WHERE project_id = ? ORDER BY created_ns,id LIMIT ?;
-- name: ReviseSwarmRun :exec
UPDATE swarm_runs SET version = ?, plan_json = ? WHERE id = ?;
-- name: CancelSwarmRun :exec
UPDATE swarm_runs SET cancelled = TRUE WHERE id = ?;
-- name: UpdateSwarmIntegrationHead :exec
UPDATE swarm_runs SET integration_head = ? WHERE id = ?;

-- name: InsertSwarmContract :exec
INSERT INTO swarm_contracts(run_id,task_id,version,contract_hash,contract_json) VALUES (?,?,?,?,?);
-- name: GetSwarmContract :one
SELECT * FROM swarm_contracts WHERE run_id = ? AND task_id = ? AND version = ?;
-- name: UpsertSwarmTask :exec
INSERT INTO swarm_tasks(run_id,task_id,version) VALUES (?,?,?)
ON CONFLICT(run_id,task_id) DO UPDATE SET version = excluded.version, accepted_attempt = '';
-- name: GetSwarmTask :one
SELECT * FROM swarm_tasks WHERE run_id = ? AND task_id = ?;
-- name: AcceptSwarmTask :exec
UPDATE swarm_tasks SET accepted_attempt = ? WHERE run_id = ? AND task_id = ? AND version = ?;

-- name: InsertSwarmAttempt :exec
INSERT INTO swarm_attempts(id,run_id,task_id,version,request_id,request_hash,operation_id,spec_json,state,owner,generation,lease_ns,created_ns)
VALUES (?,?,?,?,?,?,?,?,'reserved',?,1,?,?);
-- name: GetSwarmAttempt :one
SELECT * FROM swarm_attempts WHERE id = ?;
-- name: GetSwarmAttemptByRequest :one
SELECT * FROM swarm_attempts WHERE request_id = ?;
-- name: ListSwarmAttempts :many
SELECT * FROM swarm_attempts WHERE run_id = ? ORDER BY created_ns,id;
-- name: ListSwarmDispatches :many
SELECT * FROM swarm_attempts WHERE state IN ('reserved','dispatching','running') ORDER BY created_ns,id LIMIT ?;
-- name: CountSwarmActive :one
SELECT count(*) FROM swarm_attempts WHERE run_id = ? AND state IN ('reserved','dispatching','running');
-- name: GetSwarmOpenTaskAttempt :one
SELECT * FROM swarm_attempts WHERE run_id = ? AND task_id = ? AND state IN ('reserved','dispatching','running','submitted');
-- name: UpdateSwarmAttemptFacts :exec
UPDATE swarm_attempts SET state=?, owner=?, generation=?, lease_ns=?, session_id=?, reported_model=?, submission_commit=? WHERE id=?;

-- name: ListSwarmResources :many
SELECT * FROM swarm_resources WHERE namespace = ?;
-- name: InsertSwarmResource :exec
INSERT INTO swarm_resources(attempt_id,namespace,kind,resource_key) VALUES (?,?,?,?);
-- name: ReleaseSwarmResources :exec
DELETE FROM swarm_resources WHERE attempt_id = ?;
-- name: InsertSwarmUsage :exec
INSERT INTO swarm_usage_reservations(attempt_id,bucket,reserved) VALUES (?,?,?);
-- name: GetSwarmUsage :one
SELECT * FROM swarm_usage_reservations WHERE attempt_id = ?;
-- name: SettleSwarmUsage :exec
UPDATE swarm_usage_reservations SET actual=?,receipt_id=? WHERE attempt_id=?;

-- name: InsertSwarmEvidence :exec
INSERT INTO swarm_evidence(id,attempt_id,record_json) VALUES (?,?,?);
-- name: GetSwarmEvidence :one
SELECT * FROM swarm_evidence WHERE id = ?;
-- name: ListSwarmEvidence :many
SELECT * FROM swarm_evidence WHERE attempt_id = ? ORDER BY id;
-- name: InsertSwarmIntegration :exec
INSERT INTO swarm_integrations(id,run_id,record_json) VALUES (?,?,?);
-- name: GetSwarmIntegration :one
SELECT * FROM swarm_integrations WHERE id = ?;
