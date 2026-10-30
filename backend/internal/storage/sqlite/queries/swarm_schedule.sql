-- name: GetConfiguredSwarmScheduler :one
SELECT * FROM swarm_scheduler_policies WHERE singleton = 1;
-- name: GetSwarmSchedulerPolicy :one
SELECT * FROM swarm_scheduler_policies WHERE pool_id = ?;
-- name: InsertSwarmSchedulerPolicy :exec
INSERT INTO swarm_scheduler_policies(pool_id,policy_hash,policy_json) VALUES (?,?,?);

-- name: CountSwarmActiveAll :one
SELECT count(*) FROM swarm_attempts WHERE state IN ('reserved','dispatching','running');

-- name: InsertSwarmScheduleBinding :exec
INSERT INTO swarm_schedule_bindings(attempt_id,pool_id,provider_key,project_id,workspace_root,slot_kind) VALUES (?,?,?,?,?,?);

-- Counts intentionally ignore lease expiry and cancellation: only confirmed
-- runtime stop changes the attempt out of these states and frees capacity.
-- name: CountSwarmScheduleRole :one
SELECT count(*) FROM swarm_schedule_bindings b JOIN swarm_attempts a ON a.id = b.attempt_id
WHERE b.pool_id = ? AND b.slot_kind = ? AND a.state IN ('reserved','dispatching','running');
-- name: CountSwarmScheduleProvider :one
SELECT count(*) FROM swarm_schedule_bindings b JOIN swarm_attempts a ON a.id = b.attempt_id
WHERE b.pool_id = ? AND b.provider_key = ? AND a.state IN ('reserved','dispatching','running');
-- name: CountSwarmScheduleProject :one
SELECT count(*) FROM swarm_schedule_bindings b JOIN swarm_attempts a ON a.id = b.attempt_id
WHERE b.pool_id = ? AND b.project_id = ? AND a.state IN ('reserved','dispatching','running');
-- name: CountSwarmScheduleWorkspace :one
SELECT count(*) FROM swarm_schedule_bindings b JOIN swarm_attempts a ON a.id = b.attempt_id
WHERE b.pool_id = ? AND b.workspace_root = ? AND a.state IN ('reserved','dispatching','running');
