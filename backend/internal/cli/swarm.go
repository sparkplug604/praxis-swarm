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
	cmd.AddCommand(newSwarmPrepareCommand(c), newSwarmReviewCommand(c), newSwarmProposalCommand())
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

func newSwarmPrepareCommand(c *commandContext) *cobra.Command {
	var file, requestID string
	var execute bool
	var timeout int
	cmd := &cobra.Command{Use: "prepare", Short: "Build and validate a lead proposal; optionally execute its two assignments", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if file == "" || (execute && strings.TrimSpace(requestID) == "") {
			return usageError{errors.New("--proposal is required; --execute also requires --request-id")}
		}
		if timeout < 10 || timeout > 600 {
			return usageError{errors.New("--timeout must be between 10 and 600 seconds")}
		}
		raw, err := readSwarmInput(file)
		if err != nil {
			return usageError{err}
		}
		if !json.Valid(raw) {
			return usageError{errors.New("proposal must be valid JSON")}
		}
		var prepared struct {
			Plan    json.RawMessage     `json:"plan"`
			Context []swarmContextInput `json:"context"`
			Packets json.RawMessage     `json:"packets"`
		}
		if err := c.postJSON(cmd.Context(), "swarm/prepare", json.RawMessage(raw), &prepared); err != nil {
			return err
		}
		if !execute {
			return writeJSON(cmd.OutOrStdout(), prepared)
		}
		ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer cancel()
		return c.swarmRun(ctx, swarmStartRequest{requestID, prepared.Plan, prepared.Context, timeout}, true)
	}}
	cmd.Flags().StringVar(&file, "proposal", "", "Lead proposal JSON file")
	cmd.Flags().BoolVar(&execute, "execute", false, "Start real workers after successful preparation (consumes model usage)")
	cmd.Flags().StringVar(&requestID, "request-id", "", "Stable run identity; use get/run to recover an uncertain submission")
	cmd.Flags().IntVar(&timeout, "timeout", 240, "Daemon-owned run deadline in seconds")
	return cmd
}
func newSwarmReviewCommand(c *commandContext) *cobra.Command {
	return &cobra.Command{Use: "review ID", Short: "Assemble frozen worker changes and check instructions for lead review", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(args[0]) == "" || strings.ContainsAny(args[0], "/?#") {
			return usageError{errors.New("invalid swarm run ID")}
		}
		var raw json.RawMessage
		if err := c.postJSON(cmd.Context(), "swarm/runs/"+url.PathEscape(args[0])+"/review", nil, &raw); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), raw)
	}}
}

func newSwarmProposalCommand() *cobra.Command {
	return &cobra.Command{Use: "proposal", Short: "Print the lead proposal format and semantics", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), swarmProposalGuide)
		return err
	}}
}

const swarmProposalGuide = `Lead proposal JSON (replace every example with project-specific decisions):
{
 "projectId":"registered-project", "objective":"User goal", "specification":"Shared behavioral specification",
 "model":"explicit-user-selected-model", "effort":"low", "tokenLimit":600000,
 "tools":[{"name":"python","path":"/absolute/canonical/python3","version":"supplied-version-label"}],
 "tasks":[
  {"id":"parser","objective":"Implement the agreed parser","paths":["parser.py"],
   "nonGoals":["Do not edit tests or the other worker's files"],
   "interfaces":[{"id":"parse","contract":"Exact signature and behavior","ownership":"parser.py","errorRuleIds":["invalid"]}],
   "errors":[{"id":"invalid","when":"Describe invalid input","signal":"throw","value":"Exact exception and message","destination":"unchanged","effect":"Reject before mutation"}],
   "acceptance":{"oracleId":"parser-tests","checks":[{"id":"invalid-check","command":["python","-B","-m","unittest","tests.test_parser"],"expectedExitCode":0,"expectedResult":"Describe exact success criteria","errorRuleId":"invalid"}]},
   "progressPath":"PROGRESS.md"}
 ]
}
Add a second independent task with different ID and disjoint paths. Name declared tools in check command[0]; the daemon resolves their canonical paths and hashes. Version labels are supplied metadata, not executed version probes. Required: concrete objectives, non-goals, interfaces and exact errors; every error rule must be referenced by an interface and a check. Checks specify acceptance, not proof of execution. tokenLimit is advisory. Preparation derives sourceCommit and source manifests; hashes seal the supplied specifications, not their correctness. It executes no check commands or models. Submit with ao swarm prepare --proposal FILE; add --execute --request-id STABLE_ID only for authorized real worker execution. Review completed results with ao swarm review RUN_ID.`
