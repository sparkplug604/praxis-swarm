package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoveryCLIAndNativeProjectBinding(t *testing.T) {
	cfg := setConfigEnv(t)
	calls := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		if strings.HasPrefix(r.URL.Path, "/internal/telemetry/") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path == "/api/v1/sessions/lead-1" {
			_, _ = fmt.Fprint(w, `{"session":{"id":"lead-1","kind":"orchestrator","projectId":"project-a"}}`)
			return
		}
		if r.URL.Path != "/api/v1/swarm/discovery/project-a" {
			t.Errorf("wrong project path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPost {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if _, ok := body["expectedRevision"]; !ok {
				t.Error("missing CAS revision")
			}
		}
		_, _ = fmt.Fprint(w, `{"projectId":"project-a","revision":1,"status":"needs_input","brief":{"goal":"Build","questions":[]}}`)
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)
	path := filepath.Join(t.TempDir(), "update.json")
	if err := os.WriteFile(path, []byte(`{"expectedRevision":0,"brief":{"goal":"Build"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"swarm", "discovery", "get", "project-a", "--revision", "1"}, {"swarm", "discovery", "save", "project-a", "--input", path}} {
		out, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, args...)
		if err != nil || !strings.Contains(out, "needs_input") {
			t.Fatal(out, err)
		}
	}
	t.Setenv("AO_SESSION_ID", "lead-1")
	replies := runMCP(t, mcpCall("swarm", map[string]any{"action": "discovery_get"}), mcpCall("swarm", map[string]any{"action": "discovery_save", "discovery": map[string]any{"expectedRevision": 1, "brief": map[string]any{"goal": "Build"}}}), mcpCall("swarm", map[string]any{"action": "discovery_get", "projectId": "other"}))
	if replies[0].Result.IsError || replies[1].Result.IsError || !replies[2].Result.IsError {
		t.Fatalf("project-bound tool replies: %+v", replies)
	}
	if len(calls) == 0 {
		t.Fatal("no daemon calls")
	}
}

func TestDiscoveryNativePreservesDaemonErrors(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "lead-1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sessions/lead-1" {
			_, _ = fmt.Fprint(w, `{"session":{"id":"lead-1","kind":"orchestrator","projectId":"project-a"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"error":"not_found","message":"discovery missing; create revision one"}`)
		} else {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"error":"bad_request","message":"answer must be a string"}`)
		}
	}))
	defer server.Close()
	writeRunFileFor(t, cfg, server)
	replies := runMCP(t, mcpCall("swarm", map[string]any{"action": "discovery_get"}), mcpCall("swarm", map[string]any{"action": "discovery_save", "discovery": map[string]any{"expectedRevision": 0, "brief": map[string]any{}}}))
	for i, want := range []string{"discovery missing", "answer must be a string"} {
		if !replies[i].Result.IsError || !strings.Contains(replies[i].Result.Content[0].Text, want) {
			t.Fatalf("error masked: %+v", replies[i])
		}
	}
}
