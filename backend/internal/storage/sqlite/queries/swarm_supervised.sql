-- name: GetSupervisedSwarmRun :one
SELECT * FROM swarm_supervised_runs WHERE id = ?;

-- name: GetSupervisedSwarmRunByRequest :one
SELECT * FROM swarm_supervised_runs WHERE request_id = ?;

-- name: InsertSupervisedSwarmRun :exec
INSERT INTO swarm_supervised_runs(id, request_id, request_hash, payload_json, created_ns, updated_ns)
VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateSupervisedSwarmRun :execrows
UPDATE swarm_supervised_runs SET payload_json = ?, updated_ns = ? WHERE id = ?;
