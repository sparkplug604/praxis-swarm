package controllers_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/planning"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/supervised"
	swarmworkspace "github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/workspace"
)

type fakeSupervisedManager struct {
	input  supervised.StartInput
	id     string
	action string
	err    error
	calls  int
}

func (f *fakeSupervisedManager) response() (supervised.Run, error) {
	return supervised.Run{ID: "run-1", Profile: supervised.Profile, Status: "running", RequestID: f.input.RequestID, Tasks: []supervised.Task{{ID: "worker", SessionID: "session-1", Packet: planning.SupervisedPacket{Markdown: "task prompt"}, Observation: supervised.Observation{Model: "model", Usage: json.RawMessage(`{"tokens":7}`)}}}}, f.err
}
func (f *fakeSupervisedManager) Start(_ context.Context, in supervised.StartInput) (supervised.Run, error) {
	f.input = in
	f.action = "start"
	f.calls++
	return f.response()
}
func (f *fakeSupervisedManager) Get(_ context.Context, id string) (supervised.Run, error) {
	f.id = id
	f.action = "get"
	f.calls++
	return f.response()
}
func (f *fakeSupervisedManager) Stop(_ context.Context, id string) (supervised.Run, error) {
	f.id = id
	f.action = "stop"
	f.calls++
	return f.response()
}

func supervisedServer(t *testing.T, svc supervised.Manager) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, httpd.APIDeps{SupervisedSwarm: svc}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSupervisedSwarmAPIRequestResponseAndReplay(t *testing.T) {
	svc := &fakeSupervisedManager{}
	srv := supervisedServer(t, svc)
	input := `{"requestId":"request-1","plan":{"objective":"supplied"},"context":[{"id":"spec","body":"text"}],"timeoutSeconds":240}`
	for i := 0; i < 2; i++ {
		body, status, _ := doRequest(t, srv, "POST", "/api/v1/swarm/runs", input)
		if status != http.StatusAccepted || svc.input.RequestID != "request-1" || string(svc.input.Plan) != `{"objective":"supplied"}` || len(svc.input.Context) != 1 || svc.input.Context[0].Body != "text" || svc.input.TimeoutSeconds != 240 {
			t.Fatalf("status=%d body=%s input=%+v", status, body, svc.input)
		}
		for _, field := range []string{`"id":"run-1"`, `"markdown":"task prompt"`, `"tokens":7`, `"errors":[]`, `"limits":[]`} {
			if !strings.Contains(string(body), field) {
				t.Fatalf("missing %s in %s", field, body)
			}
		}
	}
	for _, tc := range []struct{ method, path, action string }{{"GET", "/api/v1/swarm/runs/run-1", "get"}, {"POST", "/api/v1/swarm/runs/run-1/stop", "stop"}} {
		_, status, _ := doRequest(t, srv, tc.method, tc.path, "")
		if status != http.StatusOK || svc.id != "run-1" || svc.action != tc.action {
			t.Fatalf("status=%d action=%s id=%s", status, svc.action, svc.id)
		}
	}
}

func TestSupervisedSwarmAPIRejectsInvalidBodies(t *testing.T) {
	svc := &fakeSupervisedManager{}
	srv := supervisedServer(t, svc)
	for _, body := range []string{`{`, `{"unknown":true}`, `{} {}`, `{"context":[{"id":"x","body":"a","unknown":1}]}`} {
		_, status, _ := doRequest(t, srv, "POST", "/api/v1/swarm/runs", body)
		if status != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d", body, status)
		}
	}
	_, status, _ := doRequest(t, srv, "POST", "/api/v1/swarm/runs", strings.Repeat(" ", (4<<20)+(64<<10)+1))
	if status != http.StatusRequestEntityTooLarge || svc.calls != 0 {
		t.Fatalf("status=%d calls=%d", status, svc.calls)
	}
}

func TestSupervisedSwarmAPIErrorEnvelopes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"missing", domain.ErrSwarmNotFound, 404, "SWARM_NOT_FOUND"},
		{"invalid", planning.ErrInvalid, 400, "INVALID_SWARM"},
		{"conflict", domain.ErrSwarmConflict, 409, "SWARM_CONFLICT"},
		{"unsafe-source", swarmworkspace.ErrUnsafe, 400, "INVALID_SWARM"},
		{"unsupported-source", swarmworkspace.ErrUnsupported, 400, "INVALID_SWARM"},
		{"changed-source", swarmworkspace.ErrChanged, 409, "SWARM_CONFLICT"},
		{"preserve", apierr.Forbidden("KEEP_CODE", "denied"), 403, "KEEP_CODE"},
		{"internal", errors.New("private internals"), 500, "INTERNAL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := supervisedServer(t, &fakeSupervisedManager{err: tc.err})
			body, status, _ := doRequest(t, srv, "GET", "/api/v1/swarm/runs/run-1", "")
			if status != tc.status || !strings.Contains(string(body), tc.code) || !strings.Contains(string(body), "requestId") || strings.Contains(string(body), "private internals") {
				t.Fatalf("status=%d body=%s", status, body)
			}
		})
	}
	srv := supervisedServer(t, nil)
	for _, tc := range []struct{ method, path string }{{"POST", "/api/v1/swarm/runs"}, {"GET", "/api/v1/swarm/runs/run-1"}, {"POST", "/api/v1/swarm/runs/run-1/stop"}} {
		_, status, _ := doRequest(t, srv, tc.method, tc.path, "")
		if status != http.StatusNotImplemented {
			t.Fatal(status)
		}
	}
}
