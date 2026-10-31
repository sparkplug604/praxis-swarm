package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/planning"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/supervised"
	swarmworkspace "github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/workspace"
)

// SupervisedSwarmController exposes daemon-owned runs without adding runtime policy.
type SupervisedSwarmController struct{ Svc supervised.Manager }

// Register mounts the supervised run creation, observation and stop routes.
func (c *SupervisedSwarmController) Register(r chi.Router) {
	r.Get("/swarm/discovery/{projectId}", c.discovery)
	r.Post("/swarm/discovery/{projectId}", c.discovery)
	r.Post("/swarm/runs", c.start)
	r.Post("/swarm/prepare", c.prepare)
	r.Post("/swarm/runs/{runId}/review", c.review)
	r.Get("/swarm/runs/{runId}", c.get)
	r.Post("/swarm/runs/{runId}/stop", c.stop)
}

func (c *SupervisedSwarmController) start(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/swarm/runs")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, (4<<20)+(64<<10))
	var input StartSupervisedSwarmRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		writeSupervisedJSONError(w, r, err)
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		writeSupervisedJSONError(w, r, err)
		return
	}
	suppliedContext := make([]planning.SuppliedContext, len(input.Context))
	for i, supplied := range input.Context {
		suppliedContext[i] = planning.SuppliedContext{ID: supplied.ID, Body: supplied.Body}
	}
	run, err := c.Svc.Start(r.Context(), supervised.StartInput{RequestID: input.RequestID, Plan: input.Plan, Context: suppliedContext, TimeoutSeconds: input.TimeoutSeconds})
	c.respond(w, r, run, err, http.StatusAccepted)
}

func (c *SupervisedSwarmController) get(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/swarm/runs/{runId}")
		return
	}
	run, err := c.Svc.Get(r.Context(), chi.URLParam(r, "runId"))
	c.respond(w, r, run, err, http.StatusOK)
}

func (c *SupervisedSwarmController) stop(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/swarm/runs/{runId}/stop")
		return
	}
	run, err := c.Svc.Stop(r.Context(), chi.URLParam(r, "runId"))
	c.respond(w, r, run, err, http.StatusOK)
}

func writeSupervisedJSONError(w http.ResponseWriter, r *http.Request, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		envelope.WriteAPIError(w, r, http.StatusRequestEntityTooLarge, "bad_request", "BODY_TOO_LARGE", "Swarm request body is too large", nil)
		return
	}
	envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
}

func (c *SupervisedSwarmController) respond(w http.ResponseWriter, r *http.Request, run supervised.Run, err error, status int) {
	if err != nil {
		var existing *apierr.Error
		if !errors.As(err, &existing) {
			switch {
			case errors.Is(err, domain.ErrSwarmNotFound):
				err = apierr.NotFound("SWARM_NOT_FOUND", "Swarm run not found")
			case errors.Is(err, domain.ErrSwarmInvalid), errors.Is(err, planning.ErrInvalid), errors.Is(err, planning.ErrContext), errors.Is(err, planning.ErrBudget), errors.Is(err, swarmworkspace.ErrUnsafe), errors.Is(err, swarmworkspace.ErrUnsupported):
				err = apierr.Invalid("INVALID_SWARM", err.Error(), nil)
			case errors.Is(err, domain.ErrSwarmConflict), errors.Is(err, domain.ErrSwarmStale), errors.Is(err, domain.ErrSwarmNotReady), errors.Is(err, planning.ErrStale), errors.Is(err, swarmworkspace.ErrChanged):
				err = apierr.Conflict("SWARM_CONFLICT", err.Error(), nil)
			}
		}
		envelope.WriteError(w, r, err)
		return
	}
	// Decode into the DTO to keep the complete report while the nested plan and
	// packet remain JSON documents whose validation belongs to the service.
	raw, err := json.Marshal(run)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	var out SupervisedSwarmRunResponse
	if err = json.Unmarshal(raw, &out); err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	if out.Tasks == nil {
		out.Tasks = []SupervisedSwarmTaskResponse{}
	}
	if out.Errors == nil {
		out.Errors = []string{}
	}
	if out.Limits == nil {
		out.Limits = []string{}
	}
	envelope.WriteJSON(w, status, out)
}

// prepare validates a lead proposal and returns execution inputs without launching workers.
func (c *SupervisedSwarmController) prepare(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.Svc.(interface {
		Prepare(context.Context, planning.LeadProposal) (planning.PreparedProposal, error)
	})
	if !ok {
		apispec.NotImplemented(w, r, "POST", "/api/v1/swarm/prepare")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeSupervisedJSONError(w, r, err)
		return
	}
	proposal, err := planning.ParseProposal(raw)
	if err != nil {
		c.respond(w, r, supervised.Run{}, err, http.StatusBadRequest)
		return
	}
	result, err := svc.Prepare(r.Context(), proposal)
	if err != nil {
		c.respond(w, r, supervised.Run{}, err, http.StatusBadRequest)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

func (c *SupervisedSwarmController) review(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.Svc.(interface {
		Review(context.Context, string) (supervised.ReviewBundle, error)
	})
	if !ok {
		apispec.NotImplemented(w, r, "POST", "/api/v1/swarm/runs/{runId}/review")
		return
	}
	result, err := svc.Review(r.Context(), chi.URLParam(r, "runId"))
	if err != nil {
		c.respond(w, r, supervised.Run{}, err, http.StatusBadRequest)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}
