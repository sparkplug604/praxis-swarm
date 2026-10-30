-- +goose Up

CREATE TABLE swarm_routing_accounts (
    bucket TEXT PRIMARY KEY REFERENCES swarm_budgets(bucket),
    account_id TEXT NOT NULL UNIQUE,
    policy_json TEXT NOT NULL,
    quota_json TEXT NOT NULL DEFAULT ''
);
CREATE TABLE swarm_usage_receipts (
    id TEXT PRIMARY KEY,
    bucket TEXT NOT NULL REFERENCES swarm_budgets(bucket),
    attempt_id TEXT NOT NULL REFERENCES swarm_attempts(id),
    operation_id TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    response_id TEXT NOT NULL,
    record_json TEXT NOT NULL,
    UNIQUE(bucket, provider_id, response_id)
);
CREATE INDEX swarm_usage_receipts_attempt ON swarm_usage_receipts(attempt_id);
-- +goose StatementBegin
CREATE TRIGGER swarm_routing_policy_immutable BEFORE UPDATE OF bucket,account_id,policy_json ON swarm_routing_accounts
BEGIN SELECT RAISE(ABORT, 'swarm routing policy is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER swarm_usage_receipts_immutable BEFORE UPDATE ON swarm_usage_receipts
BEGIN SELECT RAISE(ABORT, 'swarm usage receipts are immutable'); END;
-- +goose StatementEnd

-- +goose Down
DROP TABLE swarm_usage_receipts;
DROP TABLE swarm_routing_accounts;
