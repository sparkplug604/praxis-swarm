package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPSwarmProjectBoundLifecycle(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "lead-1")
	kind, project := "orchestrator", "project-a"
	starts, prepares, reviews, stops := 0, 0, 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/sessions/lead-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"id": "lead-1", "kind": kind, "projectId": project}})
		case "/api/v1/swarm/prepare":
			prepares++
			_, _ = fmt.Fprint(w, `{"plan":{"projectId":"project-a","sourceCommit":"frozen","tasks":[{"id":"one","objective":"first"}]},"context":[]}`)
		case "/api/v1/swarm/runs":
			starts++
			_, _ = fmt.Fprint(w, `{"id":"run-a","status":"running","plan":{"projectId":"project-a"},"tasks":[{"packet":{"markdown":"large private packet"}}]}`)
		case "/api/v1/swarm/runs/run-a":
			_, _ = fmt.Fprint(w, `{"id":"run-a","status":"completed","plan":{"projectId":"project-a"}}`)
		case "/api/v1/swarm/runs/run-a/review":
			reviews++
			_, _ = fmt.Fprint(w, `{"directory":"/review/files","checks":[]}`)
		case "/api/v1/swarm/runs/run-a/stop":
			stops++
			_, _ = fmt.Fprint(w, `{"id":"run-a","status":"stopped"}`)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	writeRunFileFor(t, cfg, server)
	// Exercise the actual long-lived stdio state. Prepared handle is deterministic.
	lines := []string{mcpCall("swarm", map[string]any{"action": "prepare", "proposal": map[string]any{"projectId": "other"}})}
	replies := runMCP(t, lines...)
	if !replies[0].Result.IsError || prepares != 0 {
		t.Fatal("cross-project preparation reached daemon")
	}
	// Get the prepared handle via the real server response, then reuse one stdio stream.
	replies = runMCP(t, mcpCall("swarm", map[string]any{"action": "prepare", "proposal": map[string]any{"projectId": "project-a"}}))
	var result struct {
		PreparedID string `json:"preparedId"`
	}
	if err := json.Unmarshal([]byte(replies[0].Result.Content[0].Text), &result); err != nil {
		t.Fatal(err)
	}
	calls := []string{mcpCall("swarm", map[string]any{"action": "prepare", "proposal": map[string]any{"projectId": "project-a"}}), mcpCall("swarm", map[string]any{"action": "start", "preparedId": result.PreparedID, "requestId": "stable-request"}), mcpCall("swarm", map[string]any{"action": "get", "runId": "run-a"}), mcpCall("swarm", map[string]any{"action": "review", "runId": "run-a"}), mcpCall("swarm", map[string]any{"action": "stop", "runId": "run-a"})}
	replies = runMCP(t, calls...)
	for _, reply := range replies {
		if reply.Result.IsError || reply.Error != nil {
			t.Fatalf("tool failed: %+v", reply)
		}
	}
	if starts != 1 || reviews != 1 || stops != 1 {
		t.Fatalf("wrong effects: %d %d %d", starts, reviews, stops)
	}
	if strings.Contains(replies[1].Result.Content[0].Text, "large private packet") {
		t.Fatal("expanded packet in compact report")
	}
	project = "other"
	replies = runMCP(t, mcpCall("swarm", map[string]any{"action": "stop", "runId": "run-a"}))
	if !replies[0].Result.IsError || stops != 1 {
		t.Fatal("cross-project mutation reached daemon")
	}
	kind = "worker"
	project = "project-a"
	replies = runMCP(t, mcpCall("swarm", map[string]any{"action": "prepare", "proposal": map[string]any{"projectId": "project-a"}}))
	if !replies[0].Result.IsError {
		t.Fatal("worker received lead authority")
	}
}
