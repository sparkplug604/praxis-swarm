-- +goose Up
CREATE TABLE swarm_discovery_briefs (
    project_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    payload_json TEXT NOT NULL CHECK (json_valid(payload_json)),
    PRIMARY KEY(project_id, revision)
);
-- +goose StatementBegin
CREATE TRIGGER swarm_discovery_immutable BEFORE UPDATE ON swarm_discovery_briefs
BEGIN SELECT RAISE(ABORT, 'discovery revisions are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER swarm_discovery_retained BEFORE DELETE ON swarm_discovery_briefs
BEGIN SELECT RAISE(ABORT, 'discovery revisions are retained'); END;
-- +goose StatementEnd
-- +goose Down
DROP TABLE swarm_discovery_briefs;
