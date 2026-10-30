package sqlite

import (
	"strings"
	"testing"
)

func TestSwarmExecutionMigrationPreservesStateAndImmutability(t *testing.T) {
	const version int64 = 20261029070054116
	db := openMigratedDatabaseCopy(t, version-1)
	for _, statement := range []string{
		`INSERT INTO projects(id,path,registered_at) VALUES ('existing-execution','/existing-execution','2026-10-29T00:00:00Z')`,
		`INSERT INTO swarm_budgets(bucket,token_limit,reserved,spent) VALUES ('account',1000,100,10)`,
		`INSERT INTO swarm_runs(id,request_id,request_hash,project_id,version,plan_json,integration_head,created_ns) VALUES ('r','q','hash','existing-execution',1,'{"marker":"pre-execution"}','base',1)`,
		`INSERT INTO swarm_contracts VALUES ('r','t',1,'contract-hash','{"marker":"contract"}')`,
		`INSERT INTO swarm_tasks(run_id,task_id,version) VALUES ('r','t',1)`,
		`INSERT INTO swarm_attempts(id,run_id,task_id,version,request_id,request_hash,operation_id,spec_json,state,owner,generation,lease_ns,created_ns) VALUES ('a','r','t',1,'request','hash','op','{"marker":"attempt"}','dispatching','owner',1,100,1)`,
		`INSERT INTO swarm_attempts(id,run_id,task_id,version,request_id,request_hash,operation_id,spec_json,state,owner,generation,lease_ns,created_ns) VALUES ('b','r','t',1,'request-two','hash','op-two','{"marker":"attempt-two"}','failed','owner',1,100,1)`,
		`INSERT INTO swarm_resources VALUES ('a','repo','path','src')`,
		`INSERT INTO swarm_usage_reservations(attempt_id,bucket,reserved) VALUES ('a','account',100)`,
		`INSERT INTO swarm_scheduler_policies(pool_id,policy_hash,policy_json) VALUES ('pool','hash','{"marker":"scheduler"}')`,
		`INSERT INTO swarm_schedule_bindings(attempt_id,pool_id,provider_key,project_id,workspace_root,slot_kind) VALUES ('a','pool','provider-account','existing-execution','/isolated/workspace','worker')`,
		`INSERT INTO swarm_schedule_bindings(attempt_id,pool_id,provider_key,project_id,workspace_root,slot_kind) VALUES ('b','pool','provider-account','existing-execution','/isolated/workspace-two','worker')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	assertPreserved := func() {
		t.Helper()
		for _, tc := range []struct{ query, want string }{
			{`SELECT path FROM projects WHERE id='existing-execution'`, "/existing-execution"},
			{`SELECT plan_json FROM swarm_runs WHERE id='r'`, `{"marker":"pre-execution"}`},
			{`SELECT contract_json FROM swarm_contracts WHERE run_id='r'`, `{"marker":"contract"}`},
			{`SELECT spec_json FROM swarm_attempts WHERE id='a'`, `{"marker":"attempt"}`},
			{`SELECT state FROM swarm_attempts WHERE id='a'`, "dispatching"},
			{`SELECT resource_key FROM swarm_resources WHERE attempt_id='a'`, "src"},
			{`SELECT workspace_root FROM swarm_schedule_bindings WHERE attempt_id='a'`, "/isolated/workspace"},
		} {
			var got string
			if err := db.QueryRow(tc.query).Scan(&got); err != nil || got != tc.want {
				t.Fatalf("preserved %q = %q, want %q: %v", tc.query, got, tc.want, err)
			}
		}
		var reserved, spent int64
		if err := db.QueryRow(`SELECT reserved,spent FROM swarm_budgets WHERE bucket='account'`).Scan(&reserved, &spent); err != nil || reserved != 100 || spent != 10 {
			t.Fatalf("preserved budget %d/%d: %v", reserved, spent, err)
		}
	}
	upTo(t, db, version)
	assertPreserved()
	for _, table := range []string{"swarm_execution_receipts", "swarm_execution_results"} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("new table %s = %d: %v", table, count, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO swarm_execution_receipts VALUES ('a','op','session','hash','{"marker":"receipt"}')`); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO swarm_execution_receipts VALUES ('a','op-new','session-new','hash','{}')`,
		`INSERT INTO swarm_execution_receipts VALUES ('b','op','session-new','hash','{}')`,
		`INSERT INTO swarm_execution_receipts VALUES ('b','op-new','session','hash','{}')`,
		`INSERT INTO swarm_execution_receipts VALUES ('missing','op-new','session-new','hash','{}')`,
		`INSERT INTO swarm_execution_results VALUES ('a','wrong-hash','session','{}')`,
		`INSERT INTO swarm_execution_results VALUES ('a','hash','wrong-session','{}')`,
		`INSERT INTO swarm_execution_results VALUES ('b','hash','session','{}')`,
	} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatalf("accepted conflicting or unbound record: %s", statement)
		}
	}
	if _, err := db.Exec(`INSERT INTO swarm_execution_results VALUES ('a','hash','session','{"marker":"result"}')`); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE swarm_execution_receipts SET receipt_json='changed'`,
		`UPDATE swarm_execution_receipts SET receipt_hash='changed'`,
		`UPDATE swarm_execution_receipts SET operation_id='changed'`,
		`UPDATE swarm_execution_receipts SET session_id='changed'`,
		`DELETE FROM swarm_execution_receipts`,
		`UPDATE swarm_execution_results SET result_json='changed'`,
		`UPDATE swarm_execution_results SET receipt_hash='changed'`,
		`UPDATE swarm_execution_results SET session_id='changed'`,
		`DELETE FROM swarm_execution_results`,
	} {
		if _, err := db.Exec(statement); err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Fatalf("immutable mutation %q: %v", statement, err)
		}
	}
	upTo(t, db, version)
	assertPreserved()
	downTo(t, db, version-1)
	assertPreserved()
	var remaining int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('swarm_execution_receipts','swarm_execution_results')`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("rollback left tables: %d %v", remaining, err)
	}
	upTo(t, db, version)
	assertPreserved()
}
