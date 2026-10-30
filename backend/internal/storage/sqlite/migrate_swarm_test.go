package sqlite

import (
	"strings"
	"testing"
)

func TestSwarmMigrationPreservesExistingDataAndIdentity(t *testing.T) {
	const version int64 = 20261029042228085
	db := openMigratedDatabaseCopy(t, version-1)
	if _, err := db.Exec(`INSERT INTO projects(id,path,registered_at) VALUES ('existing','/existing','2026-10-28T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	var path string
	if err := db.QueryRow(`SELECT path FROM projects WHERE id='existing'`).Scan(&path); err != nil || path != "/existing" {
		t.Fatalf("existing project = %s %v", path, err)
	}
	for _, table := range []string{"swarm_runs", "swarm_contracts", "swarm_tasks", "swarm_attempts", "swarm_resources", "swarm_budgets", "swarm_usage_reservations", "swarm_evidence", "swarm_integrations"} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s = %d %v", table, count, err)
		}
	}
	statements := []string{
		`INSERT INTO swarm_runs(id,request_id,request_hash,project_id,version,plan_json,integration_head,created_ns) VALUES ('r','q','hash','existing',1,'{}','base',1)`,
		`INSERT INTO swarm_contracts VALUES ('r','t',1,'hash','{}')`,
		`INSERT INTO swarm_attempts(id,run_id,task_id,version,request_id,request_hash,operation_id,spec_json,state,owner,generation,lease_ns,created_ns) VALUES ('a','r','t',1,'request','hash','op','{}','reserved','owner',1,100,1)`,
		`INSERT INTO swarm_evidence VALUES ('e','a','{}')`,
		`INSERT INTO swarm_integrations VALUES ('i','r','{}')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		`UPDATE swarm_contracts SET contract_json='changed'`,
		`UPDATE swarm_attempts SET operation_id='changed'`,
		`UPDATE swarm_attempts SET spec_json='changed'`,
		`UPDATE swarm_evidence SET record_json='changed'`,
		`UPDATE swarm_integrations SET record_json='changed'`,
	} {
		if _, err := db.Exec(statement); err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Fatalf("identity mutation %q: %v", statement, err)
		}
	}
	if _, err := db.Exec(`UPDATE swarm_attempts SET state='dispatching',generation=2 WHERE id='a'`); err != nil {
		t.Fatal(err)
	}
	// Reapplying is harmless; dropping only new tables also preserves the host app.
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	downTo(t, db, version-1)
	if err := db.QueryRow(`SELECT path FROM projects WHERE id='existing'`).Scan(&path); err != nil || path != "/existing" {
		t.Fatalf("rollback project = %s %v", path, err)
	}
	upTo(t, db, version)
}
