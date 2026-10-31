package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func swarmFiles(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	plan, body := filepath.Join(dir, "plan.json"), filepath.Join(dir, "context.json")
	if err := os.WriteFile(plan, []byte(`{"objective":"supplied"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(body, []byte(`[{"id":"spec","body":"supplied text"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	return plan, body
}

func TestSwarmRunRequestAndNoWait(t *testing.T) {
	cfg := setConfigEnv(t)
	plan, body := swarmFiles(t)
	var got swarmStartRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/swarm/runs" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			t.Error(r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"id":"run-1","status":"running","tasks":[{"id":"a"}],"limits":["supervised"]}`)
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)
	out, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "swarm", "run", "--plan", plan, "--context", body, "--request-id", "request-1", "--timeout", "77", "--wait=false")
	if err != nil || got.RequestID != "request-1" || got.TimeoutSeconds != 77 || len(got.Context) != 1 || got.Context[0].Body != "supplied text" || !strings.Contains(out, `"tasks"`) {
		t.Fatalf("out=%s err=%v request=%+v", out, err, got)
	}
}

func TestSwarmRunPollsAndPrintsNeedsAttention(t *testing.T) {
	cfg := setConfigEnv(t)
	plan, body := swarmFiles(t)
	gets := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/swarm/runs":
			_, _ = io.WriteString(w, `{"id":"run-1","status":"running"}`)
		case "/api/v1/swarm/runs/run-1":
			gets++
			_, _ = io.WriteString(w, `{"id":"run-1","status":"needs_attention","errors":["unknown stop"]}`)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)
	out, stderr, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "swarm", "run", "--plan", plan, "--context", body, "--request-id", "request-1")
	if ExitCode(err) != 1 || gets != 1 || !strings.Contains(out, "unknown stop") || !strings.Contains(stderr, "ao swarm stop run-1") {
		t.Fatalf("out=%s stderr=%s err=%v gets=%d", out, stderr, err, gets)
	}
}

func TestSwarmGetStopAndAPIError(t *testing.T) {
	for _, action := range []string{"get", "stop"} {
		t.Run(action, func(t *testing.T) {
			cfg := setConfigEnv(t)
			wantPath := "/api/v1/swarm/runs/run-1"
			wantMethod := http.MethodGet
			if action == "stop" {
				wantPath += "/stop"
				wantMethod = http.MethodPost
			}
			fail := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.URL.Path, "/api/v1/swarm/") {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if r.URL.Path != wantPath || r.Method != wantMethod {
					t.Errorf("%s %s", r.Method, r.URL.Path)
				}
				if fail {
					w.WriteHeader(http.StatusConflict)
					_, _ = io.WriteString(w, `{"code":"SWARM_CONFLICT","message":"changed input","requestId":"req-7"}`)
					return
				}
				_, _ = io.WriteString(w, `{"id":"run-1","status":"stopped","tasks":[]}`)
			}))
			defer srv.Close()
			writeRunFileFor(t, cfg, srv)
			out, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "swarm", action, "run-1")
			if err != nil || !strings.Contains(out, "stopped") {
				t.Fatalf("%s %v", out, err)
			}
			fail = true
			_, _, err = executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "swarm", action, "run-1")
			if ExitCode(err) != 1 || !strings.Contains(err.Error(), "changed input (SWARM_CONFLICT) [request req-7]") {
				t.Fatal(err)
			}
		})
	}
}

func TestSwarmInvalidArgsAndBoundedFiles(t *testing.T) {
	setConfigEnv(t)
	plan, body := swarmFiles(t)
	for _, args := range [][]string{{"swarm", "run"}, {"swarm", "get"}, {"swarm", "stop", "a", "b"}, {"swarm", "get", "a/b"}, {"swarm", "run", "--plan", plan, "--context", body, "--request-id", "x", "--timeout", "0"}} {
		_, _, err := executeCLI(t, Deps{}, args...)
		if ExitCode(err) != 2 {
			t.Fatalf("args=%v err=%v", args, err)
		}
	}
	for name, input := range map[string]string{"malformed": "{", "array": "[]", "trailing": "{} {}", "oversized": strings.Repeat(" ", swarmInputLimit+1)} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(plan, []byte(input), 0600); err != nil {
				t.Fatal(err)
			}
			_, _, err := executeCLI(t, Deps{}, "swarm", "run", "--plan", plan, "--context", body, "--request-id", "x")
			if ExitCode(err) != 2 {
				t.Fatal(err)
			}
		})
	}
	if _, err := readSwarmInput(t.TempDir()); err == nil {
		t.Fatal("directory accepted")
	}
	if _, err := readSwarmInput(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing file accepted")
	}
	if err := os.WriteFile(plan, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{`null`, `{}`, `[] []`, `[{"id":"a","body":"b","extra":1}]`} {
		if err := os.WriteFile(body, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		_, _, err := executeCLI(t, Deps{}, "swarm", "run", "--plan", plan, "--context", body, "--request-id", "x")
		if ExitCode(err) != 2 {
			t.Fatal(err)
		}
	}
}

func TestSwarmInterruptedWaitKeepsRunIdentity(t *testing.T) {
	cfg := setConfigEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"run-1","status":"running"}`)
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)
	var out, stderr bytes.Buffer
	c := &commandContext{deps: Deps{Out: &out, Err: &stderr, ProcessAlive: func(int) bool { return true }}.withDefaults()}
	// Cancel after the start response has been received, when its identity is printed.
	c.deps.Err = &swarmCancelWriter{Buffer: &stderr, cancel: cancel}
	err := c.swarmRun(ctx, swarmStartRequest{RequestID: "request-1", Plan: json.RawMessage(`{}`)}, true)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "ao swarm stop run-1") || out.Len() != 0 {
		t.Fatalf("out=%s err=%v", out.String(), err)
	}
}

type swarmCancelWriter struct {
	*bytes.Buffer
	cancel context.CancelFunc
}

func TestSwarmReportTerminalStatusAndIdentity(t *testing.T) {
	for _, status := range []string{"completed", "stopped", "needs_attention"} {
		t.Run(status, func(t *testing.T) {
			var out bytes.Buffer
			c := &commandContext{deps: Deps{Out: &out}}
			err := c.printSwarmReport(json.RawMessage(`{"id":"run-1","status":"`+status+`","tasks":[{"snapshotHash":"evidence"}]}`), "run-1")
			if (err != nil) != (status == "needs_attention") || !strings.Contains(out.String(), "evidence") {
				t.Fatalf("status=%s out=%s err=%v", status, out.String(), err)
			}
		})
	}
	for _, raw := range []string{`{"status":"completed"}`, `{"id":"other","status":"completed"}`, `{"id":"run-1","status":"unknown"}`} {
		if _, err := parseSwarmState(json.RawMessage(raw), "run-1"); err == nil {
			t.Fatalf("accepted response %s", raw)
		}
	}
}

func (w *swarmCancelWriter) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	w.cancel()
	return n, err
}

func TestLeadPrepareAndReviewTransport(t *testing.T) {
	cfg := setConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "proposal.json")
	if err := os.WriteFile(path, []byte(`{"projectId":"fixture"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	submits := 0
	reviews := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/swarm/prepare":
			if r.Method != http.MethodPost {
				t.Error(r.Method)
			}
			_, _ = io.WriteString(w, `{"plan":{"objective":"prepared"},"context":[],"packets":[]}`)
		case "/api/v1/swarm/runs":
			submits++
			_, _ = io.WriteString(w, `{"id":"lead-run","status":"completed"}`)
		case "/api/v1/swarm/runs/lead-run/review":
			reviews++
			if r.Method != http.MethodPost {
				t.Error(r.Method)
			}
			_, _ = io.WriteString(w, `{"directory":"/scratch/review","checks":[]}`)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)
	deps := Deps{ProcessAlive: func(int) bool { return true }}
	if _, _, err := executeCLI(t, deps, "swarm", "prepare", "--proposal", path); err != nil {
		t.Fatal(err)
	}
	if submits != 0 {
		t.Fatal("preview submitted workers")
	}
	if _, _, err := executeCLI(t, deps, "swarm", "prepare", "--proposal", path, "--execute", "--request-id", "lead-request"); err != nil {
		t.Fatal(err)
	}
	if submits != 1 {
		t.Fatal("execution did not submit exactly once")
	}
	out, _, err := executeCLI(t, deps, "swarm", "review", "lead-run")
	if err != nil || !strings.Contains(out, "/scratch/review") || reviews != 1 {
		t.Fatalf("%s %v", out, err)
	}
}
