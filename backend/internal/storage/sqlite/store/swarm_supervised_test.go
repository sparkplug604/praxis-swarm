package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

var supervisedRequestHash = strings.Repeat("a", 64)

func supervisedRawDB(t *testing.T, dir string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSupervisedSwarmRunReopenAndReplay(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	s, err := sqlitetest.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	initial := []byte(" {\"state\": \"prepared\"}\n")
	stored, created, err := s.CreateSupervisedSwarmRun(ctx, "run", "request", supervisedRequestHash, initial, swarmNow)
	if err != nil || !created || !bytes.Equal(stored, initial) {
		t.Fatalf("create = %q, %v, %v", stored, created, err)
	}
	// Returned bytes and caller input cannot modify the durable record.
	stored[0], initial[0] = 'x', 'y'
	latest := []byte(`{"state":"reported","sessionId":"observed-session"}`)
	if err := s.UpdateSupervisedSwarmRun(ctx, "run", latest, swarmNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlite.OpenPreMigrated(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSupervisedSwarmRun(ctx, "run")
	if err != nil || !bytes.Equal(got, latest) {
		t.Fatalf("reopened report = %q, %v", got, err)
	}
	got[0] = 'x'
	replayed, created, err := s.CreateSupervisedSwarmRun(ctx, "run", "request", supervisedRequestHash, []byte(`{"replace":true}`), swarmNow.Add(2*time.Minute))
	if err != nil || created || !bytes.Equal(replayed, latest) {
		t.Fatalf("replay = %q, %v, %v", replayed, created, err)
	}
	db := supervisedRawDB(t, dir)
	var id, request, hash string
	var createdNS, updatedNS int64
	if err := db.QueryRow(`SELECT id,request_id,request_hash,created_ns,updated_ns FROM swarm_supervised_runs`).Scan(&id, &request, &hash, &createdNS, &updatedNS); err != nil {
		t.Fatal(err)
	}
	if id != "run" || request != "request" || hash != supervisedRequestHash || createdNS != swarmNow.UnixNano() || updatedNS != swarmNow.Add(time.Minute).UnixNano() {
		t.Fatalf("replay changed identity or timestamps: %s %s %s %d %d", id, request, hash, createdNS, updatedNS)
	}
	for _, table := range []string{"swarm_runs", "swarm_attempts", "swarm_execution_receipts"} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("supervised report created strict records in %s: count=%d err=%v", table, count, err)
		}
	}
}

func TestSupervisedSwarmConcurrentRequestDedupe(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	left := sqlitetest.MustOpenAt(t, dir)
	right, err := sqlite.OpenPreMigrated(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = right.Close() })
	stores := []*sqlite.Store{left, right}
	const count = 12
	start := make(chan struct{})
	var wg sync.WaitGroup
	var payloads [count][]byte
	var created [count]bool
	var errs [count]error
	for i := range count {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			payloads[i], created[i], errs[i] = stores[i%2].CreateSupervisedSwarmRun(ctx, "same-run", "same-request", supervisedRequestHash, []byte(fmt.Sprintf(`{"candidate":%d}`, i)), swarmNow)
		}(i)
	}
	close(start)
	wg.Wait()
	winners := 0
	var winner []byte
	for i := range count {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if created[i] {
			winners++
			winner = payloads[i]
		}
	}
	if winners != 1 {
		t.Fatalf("created %d records for one request", winners)
	}
	for _, payload := range payloads {
		if !bytes.Equal(payload, winner) {
			t.Fatalf("replay %q differs from winner %q", payload, winner)
		}
	}
	// Competing IDs cannot each claim a shared request ID.
	start = make(chan struct{})
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, created[i], errs[i] = stores[i].CreateSupervisedSwarmRun(ctx, fmt.Sprintf("competing-%d", i), "competing-request", supervisedRequestHash, []byte(`{}`), swarmNow)
		}(i)
	}
	close(start)
	wg.Wait()
	if created[0] == created[1] {
		t.Fatalf("competing creates = %v/%v", created[0], created[1])
	}
	for i := range 2 {
		if created[i] && errs[i] != nil || !created[i] && !errors.Is(errs[i], domain.ErrSwarmConflict) {
			t.Fatalf("competing request %d: created=%v err=%v", i, created[i], errs[i])
		}
	}
}

func TestSupervisedSwarmConflictingReuseHasNoEffect(t *testing.T) {
	s := sqlitetest.MustOpen(t)
	ctx := context.Background()
	prior := []byte(`{"original":true}`)
	if _, _, err := s.CreateSupervisedSwarmRun(ctx, "run", "request", supervisedRequestHash, prior, swarmNow); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, request, hash string }{
		{"other", "request", supervisedRequestHash},
		{"run", "other", supervisedRequestHash},
		{"run", "request", strings.Repeat("b", 64)},
	} {
		stored, created, err := s.CreateSupervisedSwarmRun(ctx, tc.id, tc.request, tc.hash, []byte(`{"replacement":true}`), swarmNow.Add(time.Second))
		if !errors.Is(err, domain.ErrSwarmConflict) || created || stored != nil {
			t.Fatalf("conflict %+v = %q/%v/%v", tc, stored, created, err)
		}
	}
	got, err := s.GetSupervisedSwarmRun(ctx, "run")
	if err != nil || !bytes.Equal(got, prior) {
		t.Fatalf("conflict changed report: %q %v", got, err)
	}
	if _, err := s.GetSupervisedSwarmRun(ctx, "other"); !errors.Is(err, domain.ErrSwarmNotFound) {
		t.Fatalf("conflict created a record: %v", err)
	}
	if err := s.UpdateSupervisedSwarmRun(ctx, "missing", []byte(`{}`), swarmNow); !errors.Is(err, domain.ErrSwarmNotFound) {
		t.Fatalf("missing update: %v", err)
	}
}

func TestSupervisedSwarmRejectsInvalidInputs(t *testing.T) {
	s := sqlitetest.MustOpen(t)
	ctx := context.Background()
	for _, payload := range [][]byte{
		nil, []byte(`null`), []byte(`[]`), []byte(`"text"`), []byte(`true`), []byte(`3`),
		[]byte(`{`), []byte(`{} {}`), []byte("{\"value\":\"\xff\"}"),
		[]byte(`{"value":"` + strings.Repeat("x", 4*1024*1024) + `"}`),
	} {
		if _, created, err := s.CreateSupervisedSwarmRun(ctx, "run", "request", supervisedRequestHash, payload, swarmNow); created || !errors.Is(err, domain.ErrSwarmInvalid) {
			t.Fatalf("invalid payload create len=%d: %v/%v", len(payload), created, err)
		}
		if err := s.UpdateSupervisedSwarmRun(ctx, "run", payload, swarmNow); !errors.Is(err, domain.ErrSwarmInvalid) {
			t.Fatalf("invalid payload update len=%d: %v", len(payload), err)
		}
	}
	for _, tc := range []struct {
		id, request, hash string
		now               time.Time
	}{
		{"", "request", supervisedRequestHash, swarmNow},
		{"run", " bad ", supervisedRequestHash, swarmNow},
		{"run\n", "request", supervisedRequestHash, swarmNow},
		{strings.Repeat("x", 257), "request", supervisedRequestHash, swarmNow},
		{"run", "request", "hash", swarmNow},
		{"run", "request", strings.Repeat("A", 64), swarmNow},
		{"run", "request", supervisedRequestHash, time.Time{}},
		{"run", "request", supervisedRequestHash, time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)},
	} {
		if _, created, err := s.CreateSupervisedSwarmRun(ctx, tc.id, tc.request, tc.hash, []byte(`{}`), tc.now); created || !errors.Is(err, domain.ErrSwarmInvalid) {
			t.Fatalf("invalid identity/time create: %v/%v", created, err)
		}
	}
	if _, err := s.GetSupervisedSwarmRun(ctx, " bad "); !errors.Is(err, domain.ErrSwarmInvalid) {
		t.Fatalf("invalid get ID: %v", err)
	}
	if _, err := s.GetSupervisedSwarmRun(ctx, "run"); !errors.Is(err, domain.ErrSwarmNotFound) {
		t.Fatalf("invalid request persisted a record: %v", err)
	}
	if _, _, err := s.CreateSupervisedSwarmRun(ctx, "run", "request", supervisedRequestHash, []byte(`{}`), swarmNow); err != nil {
		t.Fatal(err)
	}
	for _, now := range []time.Time{time.Time{}, swarmNow.Add(-time.Nanosecond), time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)} {
		if err := s.UpdateSupervisedSwarmRun(ctx, "run", []byte(`{}`), now); !errors.Is(err, domain.ErrSwarmInvalid) {
			t.Fatalf("invalid update time: %v", err)
		}
	}
}

func TestSupervisedSwarmSQLiteEnforcesImmutabilityAndPayloadBound(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	// Exactly 4 MiB is accepted; the limit counts encoded bytes, not runes.
	payload := []byte(`{"x":"` + strings.Repeat("x", 4*1024*1024-8) + `"}`)
	stored, created, err := s.CreateSupervisedSwarmRun(ctx, "run", "request", supervisedRequestHash, payload, swarmNow)
	if err != nil || !created || !bytes.Equal(stored, payload) {
		t.Fatalf("boundary payload create: len=%d created=%v err=%v", len(stored), created, err)
	}
	db := supervisedRawDB(t, dir)
	for _, statement := range []string{
		`UPDATE swarm_supervised_runs SET id='changed'`,
		`UPDATE swarm_supervised_runs SET request_id='changed'`,
		`UPDATE swarm_supervised_runs SET request_hash='changed'`,
		`UPDATE swarm_supervised_runs SET created_ns=created_ns+1`,
		`DELETE FROM swarm_supervised_runs`,
		`UPDATE swarm_supervised_runs SET payload_json='null'`,
		`UPDATE swarm_supervised_runs SET payload_json='[]'`,
		`UPDATE swarm_supervised_runs SET payload_json='{'`,
		`UPDATE swarm_supervised_runs SET updated_ns=created_ns-1`,
	} {
		if _, err := db.ExecContext(ctx, statement); err == nil {
			t.Fatalf("SQLite accepted invalid mutation: %s", statement)
		}
	}
	oversized := `{"x":"` + strings.Repeat("界", 1400000) + `"}`
	if _, err := db.ExecContext(ctx, `UPDATE swarm_supervised_runs SET payload_json=?`, oversized); err == nil {
		t.Fatal("SQLite counted characters instead of encoded payload bytes")
	}
	got, err := s.GetSupervisedSwarmRun(ctx, "run")
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("failed raw mutation changed report: len=%d err=%v", len(got), err)
	}
}
