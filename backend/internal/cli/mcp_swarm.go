package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
)

// swarmLead binds the tool to the AO session supplied by the native tool host.
func (c *commandContext) swarmLead(ctx context.Context) (sessionDTO, error) {
	var response struct {
		Session sessionDTO `json:"session"`
	}
	id, err := renderSessionID()
	if err != nil {
		return response.Session, err
	}
	if err := c.getJSON(ctx, "sessions/"+url.PathEscape(id), &response); err != nil {
		return response.Session, err
	}
	if response.Session.ID != id || response.Session.Kind != "orchestrator" || response.Session.ProjectID == "" || response.Session.IsTerminated {
		return response.Session, errors.New("praxis swarm tools require an active project orchestrator session")
	}
	return response.Session, nil
}

func (c *commandContext) swarmTools(ctx context.Context) []map[string]any {
	if _, err := c.swarmLead(ctx); err != nil {
		return nil
	}
	return []map[string]any{{"name": "swarm", "description": "Coordinate Praxis Swarm through the owned daemon without shell networking. Start discovery_get/discovery_save to persist questions, supplied answers and a project brief. Ask the human material questions in chat and wait for answers. A ready brief must be referenced by revision/sha256 as proposal.brief; changes invalidate pending prepared plans. prepare validates a lead-authored proposal and returns an opaque preparedId; start consumes model usage and must be authorized. get/stop/review take runId. Only the current orchestrator's project is allowed. Review returns a combined directory/checks for you to inspect and execute with your session tools; no automatic acceptance/merge. If the tool host restarts, recover an existing run with get instead of launching a replacement.", "inputSchema": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"action"}, "properties": map[string]any{
		"action":   map[string]any{"type": "string", "enum": []string{"discovery_get", "discovery_save", "prepare", "start", "get", "stop", "review"}},
		"revision": map[string]any{"type": "integer", "minimum": 0},
		"discovery": map[string]any{"type": "object", "required": []string{"expectedRevision", "brief"}, "additionalProperties": false, "properties": map[string]any{
			"expectedRevision": map[string]any{"type": "integer", "minimum": 0},
			"brief": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
				"goal": map[string]any{"type": "string"}, "audience": map[string]any{"type": "string"},
				"outcomes":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"acceptance":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"constraints": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"assumptions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"questions": map[string]any{"type": "array", "maxItems": 16, "items": map[string]any{"type": "object", "required": []string{"id", "question", "required"}, "additionalProperties": false, "properties": map[string]any{
					"id": map[string]any{"type": "string"}, "question": map[string]any{"type": "string"}, "required": map[string]any{"type": "boolean"}, "answer": map[string]any{"type": "string", "description": "Empty string when unanswered; never null. Do not invent a human answer."},
				}}},
			}},
		}, "description": "Save complete supplied decisions and questions with the latest expectedRevision; zero creates the first revision."},
		"proposal":   map[string]any{"type": "object", "description": "The full lead-authored proposal JSON, required for prepare; get format via ao swarm proposal."},
		"preparedId": map[string]any{"type": "string"}, "requestId": map[string]any{"type": "string"}, "runId": map[string]any{"type": "string"}, "timeoutSeconds": map[string]any{"type": "integer", "minimum": 10, "maximum": 600},
	}}}}
}

type mcpPreparedSwarm struct {
	Plan    json.RawMessage     `json:"plan"`
	Context []swarmContextInput `json:"context"`
}

func (c *commandContext) callSwarm(ctx context.Context, state *mcpState, raw json.RawMessage) map[string]any {
	lead, err := c.swarmLead(ctx)
	if err != nil {
		return mcpToolError(err)
	}
	var args struct {
		Discovery      json.RawMessage `json:"discovery"`
		Revision       int64           `json:"revision"`
		Action         string          `json:"action"`
		Proposal       json.RawMessage `json:"proposal"`
		PreparedID     string          `json:"preparedId"`
		RequestID      string          `json:"requestId"`
		RunID          string          `json:"runId"`
		TimeoutSeconds int             `json:"timeoutSeconds"`
	}
	if len(raw) > swarmInputLimit {
		return mcpToolError(errors.New("swarm tool input exceeds 2 MiB"))
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return mcpToolError(err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return mcpToolError(errors.New("expected one arguments object"))
	}
	var output json.RawMessage
	switch args.Action {
	case "discovery_get":
		if args.Revision < 0 {
			return mcpToolError(errors.New("revision cannot be negative"))
		}
		err = c.getJSON(ctx, "swarm/discovery/"+url.PathEscape(lead.ProjectID)+"?revision="+strconv.FormatInt(args.Revision, 10), &output)
	case "discovery_save":
		err = c.postJSON(ctx, "swarm/discovery/"+url.PathEscape(lead.ProjectID), args.Discovery, &output)
	case "prepare":
		var proposal struct {
			ProjectID string `json:"projectId"`
		}
		if err := json.Unmarshal(args.Proposal, &proposal); err != nil {
			return mcpToolError(err)
		}
		if proposal.ProjectID != lead.ProjectID {
			return mcpToolError(errors.New("proposal project differs from this lead's project"))
		}
		if len(state.swarmPrepared) >= 8 {
			return mcpToolError(errors.New("prepared proposal limit reached; inspect existing run identities"))
		}
		var prepared mcpPreparedSwarm
		if err := c.postJSON(ctx, "swarm/prepare", args.Proposal, &prepared); err != nil {
			return mcpToolError(err)
		}
		var plan struct {
			ProjectID    string `json:"projectId"`
			SourceCommit string `json:"sourceCommit"`
			Tasks        []struct {
				ID        string `json:"id"`
				Objective string `json:"objective"`
			} `json:"tasks"`
		}
		if err := json.Unmarshal(prepared.Plan, &plan); err != nil {
			return mcpToolError(err)
		}
		if plan.ProjectID != lead.ProjectID {
			return mcpToolError(errors.New("daemon prepared another project"))
		}
		encoded, err := json.Marshal(prepared)
		if err != nil {
			return mcpToolError(err)
		}
		hash := sha256.Sum256(encoded)
		id := hex.EncodeToString(hash[:])
		if state.swarmPrepared == nil {
			state.swarmPrepared = make(map[string]mcpPreparedSwarm)
		}
		state.swarmPrepared[id] = prepared
		output, err = json.Marshal(map[string]any{"preparedId": id, "sourceCommit": plan.SourceCommit, "tasks": plan.Tasks, "status": "validated_not_started", "note": "Exact plan, contexts and preview packets retained by daemon preparation. Handle lasts for this tool host; do not re-launch uncertain runs."})
		if err != nil {
			return mcpToolError(err)
		}
	case "start":
		prepared, ok := state.swarmPrepared[args.PreparedID]
		if !ok {
			return mcpToolError(errors.New("unknown preparedId; no launch attempted"))
		}
		var binding struct {
			ProjectID string `json:"projectId"`
		}
		if err := json.Unmarshal(prepared.Plan, &binding); err != nil {
			return mcpToolError(err)
		}
		if binding.ProjectID != lead.ProjectID {
			return mcpToolError(errors.New("prepared proposal belongs to another project"))
		}
		if args.RequestID == "" {
			return mcpToolError(errors.New("start requires a stable requestId"))
		}
		if args.TimeoutSeconds == 0 {
			args.TimeoutSeconds = 240
		}
		if err := c.postJSON(ctx, "swarm/runs", swarmStartRequest{args.RequestID, prepared.Plan, prepared.Context, args.TimeoutSeconds}, &output); err != nil {
			return mcpToolError(err)
		}
	case "get", "stop", "review":
		if args.RunID == "" {
			return mcpToolError(errors.New("runId is required"))
		}
		path := "swarm/runs/" + url.PathEscape(args.RunID)
		if err := c.getJSON(ctx, path, &output); err != nil {
			return mcpToolError(err)
		}
		var run struct {
			Plan struct {
				ProjectID string `json:"projectId"`
			} `json:"plan"`
		}
		if err := json.Unmarshal(output, &run); err != nil {
			return mcpToolError(err)
		}
		if run.Plan.ProjectID != lead.ProjectID {
			return mcpToolError(errors.New("run belongs to another project"))
		}
		if args.Action != "get" {
			if err := c.postJSON(ctx, path+"/"+args.Action, nil, &output); err != nil {
				return mcpToolError(err)
			}
		}
	default:
		return mcpToolError(fmt.Errorf("unknown swarm action %q", args.Action))
	}
	if err != nil {
		return mcpToolError(err)
	}
	// Worker packets duplicate the supplied specification. Keep tool reports compact;
	// complete records remain daemon-owned and available through ordinary run evidence.
	var value map[string]any
	if err := json.Unmarshal(output, &value); err != nil {
		return mcpToolError(err)
	}
	run := value
	if nested, ok := value["run"].(map[string]any); ok {
		run = nested
	}
	if tasks, ok := run["tasks"].([]any); ok {
		for _, task := range tasks {
			if fields, ok := task.(map[string]any); ok {
				delete(fields, "packet")
			}
		}
	}
	if args.Action == "get" || args.Action == "start" || args.Action == "stop" {
		delete(run, "plan")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return mcpToolError(err)
	}
	return map[string]any{"content": []any{mcpText(string(encoded))}}
}
