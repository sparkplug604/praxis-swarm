package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const swarmInputLimit = 2 << 20

type swarmContextInput struct {
	ID   string `json:"id"`
	Body string `json:"body"`
}
type swarmStartRequest struct {
	RequestID      string              `json:"requestId"`
	Plan           json.RawMessage     `json:"plan"`
	Context        []swarmContextInput `json:"context"`
	TimeoutSeconds int                 `json:"timeoutSeconds"`
}
type swarmRunState struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func newSwarmCommand(c *commandContext) *cobra.Command {
	cmd := &cobra.Command{Use: "swarm", Short: "Run and inspect supervised AO swarms"}
	var planFile, contextFile, requestID string
	var timeout int
	var wait bool
	run := &cobra.Command{Use: "run", Short: "Submit a supplied plan and context; wait for its combined report", Args: func(_ *cobra.Command, args []string) error {
		if len(args) != 0 {
			return usageError{errors.New("swarm run takes flags, not positional arguments")}
		}
		return nil
	}, RunE: func(cmd *cobra.Command, _ []string) error {
		if planFile == "" || contextFile == "" || strings.TrimSpace(requestID) == "" || timeout < 10 || timeout > 600 {
			return usageError{errors.New("--plan, --context, --request-id and --timeout between 10 and 600 seconds are required")}
		}
		plan, err := readSwarmInput(planFile)
		if err != nil {
			return usageError{err}
		}
		if !json.Valid(plan) || len(bytes.TrimSpace(plan)) == 0 || bytes.TrimSpace(plan)[0] != '{' {
			return usageError{errors.New("--plan must contain one JSON object")}
		}
		body, err := readSwarmInput(contextFile)
		if err != nil {
			return usageError{err}
		}
		var supplied []swarmContextInput
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if len(bytes.TrimSpace(body)) == 0 || bytes.TrimSpace(body)[0] != '[' {
			return usageError{errors.New("--context must contain a JSON array of {id,body}")}
		}
		if err = dec.Decode(&supplied); err != nil {
			return usageError{fmt.Errorf("invalid context JSON: %w", err)}
		}
		if err = dec.Decode(new(any)); !errors.Is(err, io.EOF) {
			return usageError{errors.New("--context must contain one JSON array")}
		}
		ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer cancel()
		return c.swarmRun(ctx, swarmStartRequest{requestID, plan, supplied, timeout}, wait)
	}}
	run.Flags().StringVar(&planFile, "plan", "", "Supplied plan JSON file (at most 2 MiB)")
	run.Flags().StringVar(&contextFile, "context", "", "Context JSON array file (at most 2 MiB)")
	run.Flags().StringVar(&requestID, "request-id", "", "Stable idempotency key; reuse it to recover this run")
	run.Flags().IntVar(&timeout, "timeout", 240, "Daemon-owned run deadline in seconds (10–600)")
	run.Flags().BoolVar(&wait, "wait", true, "Wait for the final report; interrupting does not stop the run")
	cmd.AddCommand(run)
	for _, action := range []string{"get", "stop"} {
		cmd.AddCommand(&cobra.Command{Use: action + " ID", Short: action + " a supervised run and print its JSON report", Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 || strings.TrimSpace(args[0]) == "" || strings.ContainsAny(args[0], "/?#") {
				return usageError{errors.New("a single run ID is required")}
			}
			return nil
		}, RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer cancel()
			var raw json.RawMessage
			path := "swarm/runs/" + url.PathEscape(args[0])
			var err error
			if action == "stop" {
				err = c.postJSON(ctx, path+"/stop", nil, &raw)
			} else {
				err = c.getJSON(ctx, path, &raw)
			}
			if err != nil {
				return swarmRequestError(ctx, err, args[0], "")
			}
			return c.printSwarmReport(raw, args[0])
		}})
	}
	return cmd
}

func readSwarmInput(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Size() > swarmInputLimit {
		return nil, fmt.Errorf("%s must be a regular file of at most 2 MiB", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, swarmInputLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > swarmInputLimit {
		return nil, fmt.Errorf("%s exceeds 2 MiB", path)
	}
	return data, nil
}

func (c *commandContext) swarmRun(ctx context.Context, input swarmStartRequest, wait bool) error {
	var raw json.RawMessage
	if err := c.postJSON(ctx, "swarm/runs", input, &raw); err != nil {
		return swarmRequestError(ctx, err, "", input.RequestID)
	}
	state, err := parseSwarmState(raw, "")
	if err != nil {
		return err
	}
	id := state.ID
	// Preserve the durable identity on stderr while stdout stays one JSON report.
	if wait && state.Status == "running" {
		_, _ = fmt.Fprintf(c.deps.Err, "Swarm run %s; interrupting this wait does not stop it. Stop with: ao swarm stop %s\n", id, id)
	}
	for wait && state.Status == "running" {
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return swarmRequestError(ctx, ctx.Err(), id, input.RequestID)
		case <-timer.C:
		}
		if err = c.getJSON(ctx, "swarm/runs/"+url.PathEscape(id), &raw); err != nil {
			return swarmRequestError(ctx, err, id, input.RequestID)
		}
		state, err = parseSwarmState(raw, id)
		if err != nil {
			return err
		}
	}
	return c.printSwarmReport(raw, id)
}

func parseSwarmState(raw json.RawMessage, expectedID string) (swarmRunState, error) {
	var state swarmRunState
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, err
	}
	if state.ID == "" || strings.ContainsAny(state.ID, "/?#\r\n") || (expectedID != "" && state.ID != expectedID) {
		return state, errors.New("daemon returned a missing or mismatched swarm run ID")
	}
	switch state.Status {
	case "running", "completed", "stopped", "needs_attention":
		return state, nil
	default:
		return state, fmt.Errorf("run %s returned unknown status %q", state.ID, state.Status)
	}
}

func (c *commandContext) printSwarmReport(raw json.RawMessage, expectedID string) error {
	state, err := parseSwarmState(raw, expectedID)
	if err != nil {
		return err
	}
	if err := writeJSON(c.deps.Out, raw); err != nil {
		return err
	}
	if state.Status == "needs_attention" {
		return fmt.Errorf("swarm run %s needs attention; inspect the JSON report", state.ID)
	}
	return nil
}

func swarmRequestError(ctx context.Context, err error, id, requestID string) error {
	if id != "" {
		return fmt.Errorf("%w; run %s may still be active; inspect with ao swarm get %s or stop with ao swarm stop %s", err, id, id, id)
	}
	if ctx.Err() != nil || errors.Is(err, errDaemonUnavailable) {
		return fmt.Errorf("%w; submission outcome is unknown; retry the same --request-id %s to recover the run", err, requestID)
	}
	return err
}
