-- +goose Up
CREATE TABLE swarm_execution_receipts (
    attempt_id TEXT PRIMARY KEY REFERENCES swarm_schedule_bindings(attempt_id),
    operation_id TEXT NOT NULL UNIQUE,
    session_id TEXT NOT NULL UNIQUE,
    receipt_hash TEXT NOT NULL,
    receipt_json TEXT NOT NULL,
    UNIQUE(attempt_id, receipt_hash, session_id)
);
CREATE TABLE swarm_execution_results (
    attempt_id TEXT PRIMARY KEY,
    receipt_hash TEXT NOT NULL,
    session_id TEXT NOT NULL,
    result_json TEXT NOT NULL,
    FOREIGN KEY(attempt_id, receipt_hash, session_id)
        REFERENCES swarm_execution_receipts(attempt_id, receipt_hash, session_id)
);
-- +goose StatementBegin
CREATE TRIGGER swarm_execution_receipt_immutable BEFORE UPDATE ON swarm_execution_receipts
BEGIN SELECT RAISE(ABORT, 'swarm execution receipt is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER swarm_execution_receipt_retained BEFORE DELETE ON swarm_execution_receipts
BEGIN SELECT RAISE(ABORT, 'swarm execution receipt is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER swarm_execution_result_immutable BEFORE UPDATE ON swarm_execution_results
BEGIN SELECT RAISE(ABORT, 'swarm execution result is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER swarm_execution_result_retained BEFORE DELETE ON swarm_execution_results
BEGIN SELECT RAISE(ABORT, 'swarm execution result is immutable'); END;
-- +goose StatementEnd

-- +goose Down
DROP TABLE swarm_execution_results;
DROP TABLE swarm_execution_receipts;
