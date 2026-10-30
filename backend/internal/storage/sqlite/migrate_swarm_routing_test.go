package sqlite

import (
	"database/sql"
	"strings"
	"testing"
)

func TestSwarmRoutingMigrationPreservesExistingDataAndIdentity(t *testing.T) {
	const version int64 = 20261029054255041
	db := openMigratedDatabaseCopy(t, version-1)
	for _, statement := range []string{
		`INSERT INTO projects(id,path,registered_at) VALUES ('existing-routing','/existing-routing','2026-10-28T00:00:00Z')`,
		`INSERT INTO swarm_budgets(bucket,token_limit,reserved,spent) VALUES ('alpha',1000,100,10),('beta',2000,0,0)`,
		`INSERT INTO swarm_runs(id,request_id,request_hash,project_id,version,plan_json,integration_head,created_ns) VALUES ('r','q','hash','existing-routing',1,'{"marker":"pre-routing"}','base',1)`,
		`INSERT INTO swarm_contracts VALUES ('r','t',1,'contract-hash','{"marker":"contract"}')`,
		`INSERT INTO swarm_tasks(run_id,task_id,version) VALUES ('r','t',1)`,
		`INSERT INTO swarm_attempts(id,run_id,task_id,version,request_id,request_hash,operation_id,spec_json,state,owner,generation,lease_ns,created_ns) VALUES ('a','r','t',1,'request','hash','op','{"marker":"attempt"}','reserved','owner',1,100,1)`,
		`INSERT INTO swarm_resources VALUES ('a','repo','path','src')`,
		`INSERT INTO swarm_usage_reservations(attempt_id,bucket,reserved) VALUES ('a','alpha',100)`,
		`INSERT INTO swarm_evidence VALUES ('e','a','{"marker":"evidence"}')`,
		`INSERT INTO swarm_integrations VALUES ('i','r','{"marker":"integration"}')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed existing state: %v", err)
		}
	}
	upTo(t, db, version)
	assertSwarmRoutingMigrationExistingData(t, db)
	for _, table := range []string{"swarm_routing_accounts", "swarm_usage_receipts"} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("new %s table: count=%d err=%v", table, count, err)
		}
	}
	for _, statement := range []string{
		`INSERT INTO swarm_routing_accounts(bucket,account_id,policy_json) VALUES ('alpha','provider-account','{"marker":"policy"}')`,
		`INSERT INTO swarm_usage_receipts(id,bucket,attempt_id,operation_id,provider_id,response_id,record_json) VALUES ('receipt','alpha','a','op','provider','response','{"marker":"usage"}')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		`INSERT INTO swarm_routing_accounts(bucket,account_id,policy_json) VALUES ('beta','provider-account','{}')`,
		`INSERT INTO swarm_usage_receipts(id,bucket,attempt_id,operation_id,provider_id,response_id,record_json) VALUES ('receipt-redelivery','alpha','a','op','provider','response','{}')`,
		`INSERT INTO swarm_usage_receipts(id,bucket,attempt_id,operation_id,provider_id,response_id,record_json) VALUES ('receipt','alpha','a','op','other-provider','other-response','{}')`,
	} {
		if _, err := db.Exec(statement); err == nil || !strings.Contains(strings.ToLower(err.Error()), "unique") {
			t.Fatalf("duplicate account/response identity accepted for %q: %v", statement, err)
		}
	}
	// Distinct provider identities can reuse provider-local response IDs.
	if _, err := db.Exec(`INSERT INTO swarm_usage_receipts(id,bucket,attempt_id,operation_id,provider_id,response_id,record_json) VALUES ('receipt-other-provider','alpha','a','op','other-provider','response','{}')`); err != nil {
		t.Fatalf("independent provider response rejected: %v", err)
	}
	for _, statement := range []string{
		`UPDATE swarm_routing_accounts SET policy_json='changed' WHERE bucket='alpha'`,
		`UPDATE swarm_routing_accounts SET account_id='changed' WHERE bucket='alpha'`,
		`UPDATE swarm_routing_accounts SET bucket='beta' WHERE bucket='alpha'`,
		`UPDATE swarm_usage_receipts SET record_json='changed' WHERE id='receipt'`,
		`UPDATE swarm_usage_receipts SET response_id='changed' WHERE id='receipt'`,
		`UPDATE swarm_usage_receipts SET operation_id='changed' WHERE id='receipt'`,
		`UPDATE swarm_usage_receipts SET attempt_id='changed' WHERE id='receipt'`,
	} {
		if _, err := db.Exec(statement); err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Fatalf("immutable identity mutation %q: %v", statement, err)
		}
	}
	if _, err := db.Exec(`UPDATE swarm_routing_accounts SET quota_json='{"revision":2}' WHERE bucket='alpha'`); err != nil {
		t.Fatalf("quota refresh blocked by policy immutability: %v", err)
	}
	var policy, account, quota, receipt string
	if err := db.QueryRow(`SELECT account_id,policy_json,quota_json FROM swarm_routing_accounts WHERE bucket='alpha'`).Scan(&account, &policy, &quota); err != nil || account != "provider-account" || policy != `{"marker":"policy"}` || quota != `{"revision":2}` {
		t.Fatalf("preserved routing identity: account=%q policy=%q quota=%q err=%v", account, policy, quota, err)
	}
	if err := db.QueryRow(`SELECT record_json FROM swarm_usage_receipts WHERE id='receipt'`).Scan(&receipt); err != nil || receipt != `{"marker":"usage"}` {
		t.Fatalf("preserved receipt=%q err=%v", receipt, err)
	}
	upTo(t, db, version)
	assertSwarmRoutingMigrationExistingData(t, db)
	downTo(t, db, version-1)
	assertSwarmRoutingMigrationExistingData(t, db)
	var remaining int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('swarm_routing_accounts','swarm_usage_receipts')`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("routing rollback left new tables: %d %v", remaining, err)
	}
	upTo(t, db, version)
	assertSwarmRoutingMigrationExistingData(t, db)
	for _, table := range []string{"swarm_routing_accounts", "swarm_usage_receipts"} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("reapplied %s table: count=%d err=%v", table, count, err)
		}
	}
}

func assertSwarmRoutingMigrationExistingData(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, tt := range []struct {
		query string
		want  string
	}{
		{`SELECT path FROM projects WHERE id='existing-routing'`, "/existing-routing"},
		{`SELECT plan_json FROM swarm_runs WHERE id='r'`, `{"marker":"pre-routing"}`},
		{`SELECT contract_json FROM swarm_contracts WHERE run_id='r' AND task_id='t'`, `{"marker":"contract"}`},
		{`SELECT spec_json FROM swarm_attempts WHERE id='a'`, `{"marker":"attempt"}`},
		{`SELECT operation_id FROM swarm_attempts WHERE id='a'`, "op"},
		{`SELECT state FROM swarm_attempts WHERE id='a'`, "reserved"},
		{`SELECT resource_key FROM swarm_resources WHERE attempt_id='a'`, "src"},
		{`SELECT record_json FROM swarm_evidence WHERE id='e'`, `{"marker":"evidence"}`},
		{`SELECT record_json FROM swarm_integrations WHERE id='i'`, `{"marker":"integration"}`},
	} {
		var got string
		if err := db.QueryRow(tt.query).Scan(&got); err != nil || got != tt.want {
			t.Fatalf("preservation %s: got=%q want=%q err=%v", tt.query, got, tt.want, err)
		}
	}
	var limit, reserved, spent, taskVersion, actual, usageReserved int64
	if err := db.QueryRow(`SELECT token_limit,reserved,spent FROM swarm_budgets WHERE bucket='alpha'`).Scan(&limit, &reserved, &spent); err != nil || limit != 1000 || reserved != 100 || spent != 10 {
		t.Fatalf("preserved budget=(%d,%d,%d) err=%v", limit, reserved, spent, err)
	}
	if err := db.QueryRow(`SELECT version FROM swarm_tasks WHERE run_id='r' AND task_id='t'`).Scan(&taskVersion); err != nil || taskVersion != 1 {
		t.Fatalf("preserved task version=%d err=%v", taskVersion, err)
	}
	if err := db.QueryRow(`SELECT reserved,actual FROM swarm_usage_reservations WHERE attempt_id='a'`).Scan(&usageReserved, &actual); err != nil || usageReserved != 100 || actual != -1 {
		t.Fatalf("preserved unknown usage=(%d,%d) err=%v", usageReserved, actual, err)
	}
}
