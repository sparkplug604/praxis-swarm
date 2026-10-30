-- +goose Up
-- Supervised launch/report intent is separate from strict swarm attempts and
-- qualification. Like the other swarm tables it is read through polling; no
-- session/PR CDC event is fabricated for this profile record.
CREATE TABLE swarm_supervised_runs (
    id TEXT PRIMARY KEY NOT NULL,
    request_id TEXT NOT NULL UNIQUE,
    request_hash TEXT NOT NULL,
    payload_json TEXT NOT NULL
        CHECK (length(CAST(payload_json AS BLOB)) BETWEEN 2 AND 4194304)
        CHECK (json_valid(payload_json) AND json_type(payload_json) = 'object'),
    created_ns INTEGER NOT NULL,
    updated_ns INTEGER NOT NULL CHECK (updated_ns >= created_ns)
);

-- +goose StatementBegin
CREATE TRIGGER swarm_supervised_identity_immutable
BEFORE UPDATE OF id, request_id, request_hash, created_ns ON swarm_supervised_runs
BEGIN SELECT RAISE(ABORT, 'supervised swarm identity is immutable'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER swarm_supervised_run_retained BEFORE DELETE ON swarm_supervised_runs
BEGIN SELECT RAISE(ABORT, 'supervised swarm run is retained'); END;
-- +goose StatementEnd

-- +goose Down
DROP TABLE swarm_supervised_runs;
