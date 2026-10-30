-- name: GetSwarmExecutionReceipt :one
SELECT * FROM swarm_execution_receipts WHERE attempt_id = ?;
-- name: FindSwarmExecutionIdentities :many
SELECT * FROM swarm_execution_receipts WHERE attempt_id = ? OR operation_id = ? OR session_id = ?;
-- name: InsertSwarmExecutionReceipt :exec
INSERT INTO swarm_execution_receipts(attempt_id,operation_id,session_id,receipt_hash,receipt_json)
VALUES (?,?,?,?,?);
-- name: GetSwarmExecutionSchedule :one
SELECT * FROM swarm_schedule_bindings WHERE attempt_id = ?;
-- name: GetSwarmExecutionResult :one
SELECT * FROM swarm_execution_results WHERE attempt_id = ?;
-- name: InsertSwarmExecutionResult :exec
INSERT INTO swarm_execution_results(attempt_id,receipt_hash,session_id,result_json) VALUES (?,?,?,?);
