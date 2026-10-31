-- name: LatestSwarmDiscovery :one
SELECT payload_json FROM swarm_discovery_briefs WHERE project_id = ? ORDER BY revision DESC LIMIT 1;

-- name: GetSwarmDiscoveryRevision :one
SELECT payload_json FROM swarm_discovery_briefs WHERE project_id = ? AND revision = ?;

-- name: InsertSwarmDiscovery :exec
INSERT INTO swarm_discovery_briefs(project_id, revision, payload_json) VALUES (?, ?, ?);
