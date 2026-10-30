package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

const maxSupervisedSwarmPayloadBytes = 4 * 1024 * 1024

func validSupervisedSwarmPayload(payload []byte) bool {
	if len(payload) > maxSupervisedSwarmPayloadBytes || !utf8.Valid(payload) || !json.Valid(payload) {
		return false
	}
	trimmed := bytes.TrimSpace(payload)
	return len(trimmed) >= 2 && trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}'
}

func validSupervisedSwarmTime(now time.Time) bool {
	return !now.IsZero() && time.Unix(0, now.UnixNano()).Equal(now)
}

// CreateSupervisedSwarmRun durably records one supervised request. Replaying
// the same id/request/hash returns the current stored report without replacing
// it or authorizing another launch. It creates no strict swarm attempt or
// qualification record; the supervised service owns serialized report updates.
func (s *Store) CreateSupervisedSwarmRun(ctx context.Context, id, requestID, requestHash string, payload []byte, now time.Time) (stored []byte, created bool, err error) {
	if !domain.SwarmValidID(id) || !domain.SwarmValidID(requestID) || !domain.SwarmValidHash(requestHash, false) ||
		!validSupervisedSwarmPayload(payload) || !validSupervisedSwarmTime(now) {
		return nil, false, domain.ErrSwarmInvalid
	}
	data := string(payload)
	err = s.swarmWrite(ctx, func(q *gen.Queries) error {
		prior, err := q.GetSupervisedSwarmRunByRequest(ctx, requestID)
		if err == nil {
			if prior.ID != id || prior.RequestHash != requestHash {
				return domain.ErrSwarmConflict
			}
			stored = []byte(prior.PayloadJson)
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err = q.GetSupervisedSwarmRun(ctx, id); err == nil {
			return domain.ErrSwarmConflict
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := q.InsertSupervisedSwarmRun(ctx, gen.InsertSupervisedSwarmRunParams{
			ID: id, RequestID: requestID, RequestHash: requestHash,
			PayloadJson: data, CreatedNs: now.UnixNano(), UpdatedNs: now.UnixNano(),
		}); err != nil {
			return err
		}
		stored, created = []byte(data), true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return stored, created, nil
}

// GetSupervisedSwarmRun returns an independent copy of the durable report.
func (s *Store) GetSupervisedSwarmRun(ctx context.Context, id string) ([]byte, error) {
	if !domain.SwarmValidID(id) {
		return nil, domain.ErrSwarmInvalid
	}
	row, err := s.qr.GetSupervisedSwarmRun(ctx, id)
	if err != nil {
		return nil, swarmErr(err)
	}
	return []byte(row.PayloadJson), nil
}

// UpdateSupervisedSwarmRun changes only the report and its update timestamp.
// The service serializes updates; request identity stays immutable in SQLite.
func (s *Store) UpdateSupervisedSwarmRun(ctx context.Context, id string, payload []byte, now time.Time) error {
	if !domain.SwarmValidID(id) || !validSupervisedSwarmPayload(payload) || !validSupervisedSwarmTime(now) {
		return domain.ErrSwarmInvalid
	}
	data := string(payload)
	return s.swarmWrite(ctx, func(q *gen.Queries) error {
		prior, err := q.GetSupervisedSwarmRun(ctx, id)
		if err != nil {
			return swarmErr(err)
		}
		if now.UnixNano() < prior.CreatedNs {
			return domain.ErrSwarmInvalid
		}
		count, err := q.UpdateSupervisedSwarmRun(ctx, gen.UpdateSupervisedSwarmRunParams{
			PayloadJson: data, UpdatedNs: now.UnixNano(), ID: id,
		})
		if err != nil {
			return err
		}
		if count != 1 {
			return domain.ErrSwarmNotFound
		}
		return nil
	})
}
