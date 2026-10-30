package sqlite

import (
	"strings"
	"testing"
)

func TestSwarmSchedulerMigrationPreservesExistingDataAndIdentity(t *testing.T) {
	const version int64 = 20261029062138749
	db := openMigratedDatabaseCopy(t, version-1)
	for _, statement := range []string{
		`INSERT INTO projects(id,path,registered_at) VALUES ('existing-scheduler','/existing-scheduler','2026-10-28T00:00:00Z')`,
		`INSERT INTO swarm_budgets(bucket,token_limit,reserved,spent) VALUES ('account',1000,100,10)`,
		`INSERT INTO swarm_runs(id,request_id,request_hash,project_id,version,plan_json,integration_head,created_ns) VALUES ('r','q','hash','existing-scheduler',1,'{"marker":"pre-scheduler"}','base',1)`,
		`INSERT INTO swarm_contracts VALUES ('r','t',1,'contract-hash','{"marker":"contract"}')`,
		`INSERT INTO swarm_tasks(run_id,task_id,version) VALUES ('r','t',1)`,
		`INSERT INTO swarm_attempts(id,run_id,task_id,version,request_id,request_hash,operation_id,spec_json,state,owner,generation,lease_ns,created_ns) VALUES ('a','r','t',1,'request','hash','op','{"marker":"attempt"}','reserved','owner',1,100,1)`,
		`INSERT INTO swarm_resources VALUES ('a','repo','path','src')`,
		`INSERT INTO swarm_usage_reservations(attempt_id,bucket,reserved) VALUES ('a','account',100)`,
		`INSERT INTO swarm_routing_accounts(bucket,account_id,policy_json,quota_json) VALUES ('account','provider-account','{"marker":"policy"}','{"marker":"quota"}')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	assertOriginal := func() {
		t.Helper()
		for _, test := range []struct{ query, want string }{
			{`SELECT path FROM projects WHERE id='existing-scheduler'`, "/existing-scheduler"},
			{`SELECT plan_json FROM swarm_runs WHERE id='r'`, `{"marker":"pre-scheduler"}`},
			{`SELECT contract_json FROM swarm_contracts WHERE run_id='r'`, `{"marker":"contract"}`},
			{`SELECT spec_json FROM swarm_attempts WHERE id='a'`, `{"marker":"attempt"}`},
			{`SELECT state FROM swarm_attempts WHERE id='a'`, "reserved"},
			{`SELECT resource_key FROM swarm_resources WHERE attempt_id='a'`, "src"},
			{`SELECT policy_json FROM swarm_routing_accounts WHERE bucket='account'`, `{"marker":"policy"}`},
			{`SELECT quota_json FROM swarm_routing_accounts WHERE bucket='account'`, `{"marker":"quota"}`},
		} {
			var got string
			if err := db.QueryRow(test.query).Scan(&got); err != nil || got != test.want {
				t.Fatalf("preserved %q = %q, want %q: %v", test.query, got, test.want, err)
			}
		}
		var reserved, spent, usage int64
		if err := db.QueryRow(`SELECT reserved,spent FROM swarm_budgets WHERE bucket='account'`).Scan(&reserved, &spent); err != nil || reserved != 100 || spent != 10 {
			t.Fatalf("preserved budget %d/%d: %v", reserved, spent, err)
		}
		if err := db.QueryRow(`SELECT reserved FROM swarm_usage_reservations WHERE attempt_id='a'`).Scan(&usage); err != nil || usage != 100 {
			t.Fatalf("preserved usage %d: %v", usage, err)
		}
	}
	upTo(t, db, version)
	assertOriginal()
	for _, table := range []string{"swarm_scheduler_policies", "swarm_schedule_bindings"} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("new table %s: %d %v", table, count, err)
		}
	}
	for _, statement := range []string{
		`INSERT INTO swarm_scheduler_policies(pool_id,policy_hash,policy_json) VALUES ('pool','hash','{"marker":"scheduler"}')`,
		`INSERT INTO swarm_schedule_bindings(attempt_id,pool_id,provider_key,project_id,workspace_root,slot_kind) VALUES ('a','pool','provider-account','existing-scheduler','/isolated/workspace','worker')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO swarm_scheduler_policies(pool_id,policy_hash,policy_json) VALUES ('other','hash','{}')`); err == nil || !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("second global pool was accepted: %v", err)
	}
	for _, statement := range []string{
		`UPDATE swarm_scheduler_policies SET policy_json='changed'`,
		`UPDATE swarm_scheduler_policies SET policy_hash='changed'`,
		`UPDATE swarm_scheduler_policies SET pool_id='changed'`,
		`UPDATE swarm_schedule_bindings SET provider_key='changed'`,
		`UPDATE swarm_schedule_bindings SET project_id='changed'`,
		`UPDATE swarm_schedule_bindings SET workspace_root='changed'`,
		`UPDATE swarm_schedule_bindings SET slot_kind='reviewer'`,
	} {
		if _, err := db.Exec(statement); err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Fatalf("immutable scheduler identity mutation %q: %v", statement, err)
		}
	}
	upTo(t, db, version)
	assertOriginal()
	downTo(t, db, version-1)
	assertOriginal()
	var remaining int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('swarm_scheduler_policies','swarm_schedule_bindings')`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("scheduler rollback left tables: %d %v", remaining, err)
	}
	upTo(t, db, version)
	assertOriginal()
}
