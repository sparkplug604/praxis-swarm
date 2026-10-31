package controllers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/planning"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/supervised"
)

// SwarmDiscoveryProjectParam describes versioned project discovery data.
type SwarmDiscoveryProjectParam struct {
	ProjectID string `path:"projectId"`
}

// SwarmDiscoveryQuery describes versioned project discovery data.
type SwarmDiscoveryQuery struct {
	Revision int64 `query:"revision" minimum:"0"`
}

type discoveryManager interface {
	Discovery(context.Context, domain.ProjectID, int64) (domain.SwarmDiscoveryBrief, error)
	SaveDiscovery(context.Context, domain.ProjectID, domain.SwarmDiscoveryUpdate) (domain.SwarmDiscoveryBrief, error)
}

func (c *SupervisedSwarmController) discovery(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.Svc.(discoveryManager)
	if !ok {
		apispec.NotImplemented(w, r, r.Method, "/api/v1/swarm/discovery/{projectId}")
		return
	}
	project := domain.ProjectID(chi.URLParam(r, "projectId"))
	var out domain.SwarmDiscoveryBrief
	var err error
	if r.Method == http.MethodGet {
		revision := int64(0)
		if value := r.URL.Query().Get("revision"); value != "" {
			revision, err = strconv.ParseInt(value, 10, 64)
			if err != nil || revision < 0 {
				c.respond(w, r, supervised.Run{}, domain.ErrSwarmInvalid, http.StatusBadRequest)
				return
			}
		}
		out, err = svc.Discovery(r.Context(), project, revision)
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, 96*1024)
		raw, e := io.ReadAll(r.Body)
		if e != nil {
			writeSupervisedJSONError(w, r, e)
			return
		}
		update, e := planning.ParseDiscoveryUpdate(raw)
		if e != nil {
			c.respond(w, r, supervised.Run{}, e, http.StatusBadRequest)
			return
		}
		out, err = svc.SaveDiscovery(r.Context(), project, update)
	}
	if errors.Is(err, domain.ErrSwarmNotFound) {
		envelope.WriteAPIError(w, r, http.StatusNotFound, "not_found", "DISCOVERY_NOT_FOUND", "Project or discovery brief not found; a registered project can create revision one", nil)
		return
	}
	if err != nil {
		c.respond(w, r, supervised.Run{}, err, http.StatusBadRequest)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, out)
}
