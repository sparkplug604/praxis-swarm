package controllers_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
)

func (f *fakeSupervisedManager) Discovery(_ context.Context, id domain.ProjectID, revision int64) (domain.SwarmDiscoveryBrief, error) {
	f.calls++
	f.id = string(id)
	f.action = "discovery_get"
	if revision == 0 {
		revision = 1
	}
	return domain.SwarmDiscoveryBrief{ProjectID: id, Revision: revision, Status: "needs_input"}, f.err
}
func (f *fakeSupervisedManager) SaveDiscovery(_ context.Context, id domain.ProjectID, u domain.SwarmDiscoveryUpdate) (domain.SwarmDiscoveryBrief, error) {
	f.calls++
	f.id = string(id)
	f.action = "discovery_save"
	b, e := domain.NewSwarmDiscoveryBrief(id, u.ExpectedRevision+1, u.Brief, time.Now())
	if f.err != nil {
		return b, f.err
	}
	return b, e
}
func TestDiscoveryHTTPBoundary(t *testing.T) {
	for _, tc := range []struct {
		method, path, body string
		status             int
		calls              int
	}{
		{"GET", "/swarm/discovery/project?revision=2", "", 200, 1},
		{"POST", "/swarm/discovery/project", `{"expectedRevision":0,"brief":{"goal":"Build"}}`, 200, 1},
		{"GET", "/swarm/discovery/project?revision=-1", "", 400, 0},
		{"POST", "/swarm/discovery/project", `{"expectedRevision":0,"expectedRevision":1}`, 400, 0},
		{"POST", "/swarm/discovery/project", `{"brief":{"Goal":"Alias"}}`, 400, 0},
		{"POST", "/swarm/discovery/project", strings.Repeat(" ", 96*1024+1), 413, 0},
	} {
		t.Run(tc.method+tc.path+tc.body[:min(len(tc.body), 20)], func(t *testing.T) {
			fake := &fakeSupervisedManager{}
			router := chi.NewRouter()
			(&controllers.SupervisedSwarmController{Svc: fake}).Register(router)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if response.Code != tc.status || fake.calls != tc.calls {
				t.Fatal(response.Code, response.Body.String(), fake.calls)
			}
			if response.Code == 200 {
				var brief domain.SwarmDiscoveryBrief
				if err := json.Unmarshal(response.Body.Bytes(), &brief); err != nil || brief.ProjectID != "project" {
					t.Fatal(brief, err)
				}
			}
		})
	}
}
