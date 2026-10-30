-- name: InsertSwarmRoutingAccount :exec
INSERT INTO swarm_routing_accounts(bucket,account_id,policy_json) VALUES (?,?,?);
-- name: GetSwarmRoutingAccount :one
SELECT * FROM swarm_routing_accounts WHERE bucket=?;
-- name: GetSwarmRoutingAccountByIdentity :one
SELECT * FROM swarm_routing_accounts WHERE account_id=?;
-- name: UpdateSwarmQuota :exec
UPDATE swarm_routing_accounts SET quota_json=? WHERE bucket=?;
-- name: ListSwarmBudgetUsage :many
SELECT u.attempt_id,u.reserved,u.actual,a.spec_json,a.reported_model FROM swarm_usage_reservations u
JOIN swarm_attempts a ON a.id=u.attempt_id WHERE u.bucket=? ORDER BY u.attempt_id;
-- name: ListSwarmUsageReceipts :many
SELECT * FROM swarm_usage_receipts WHERE bucket=? ORDER BY id;
-- name: ListSwarmAttemptReceipts :many
SELECT * FROM swarm_usage_receipts WHERE attempt_id=? ORDER BY id;
-- name: GetSwarmUsageReceipt :one
SELECT * FROM swarm_usage_receipts WHERE id=?;
-- name: GetSwarmResponseReceipt :one
SELECT * FROM swarm_usage_receipts WHERE bucket=? AND provider_id=? AND response_id=?;
-- name: InsertSwarmUsageReceipt :exec
INSERT INTO swarm_usage_receipts(id,bucket,attempt_id,operation_id,provider_id,response_id,record_json)
VALUES (?,?,?,?,?,?,?);
