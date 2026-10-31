package planning

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ParseDiscoveryUpdate rejects ambiguous, malformed and unbounded JSON.
func ParseDiscoveryUpdate(raw []byte) (domain.SwarmDiscoveryUpdate, error) {
	var out domain.SwarmDiscoveryUpdate
	if len(raw) == 0 || len(raw) > 96*1024 || !utf8.Valid(raw) {
		return out, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := planningScanJSON(d, reflect.TypeOf(out), 0); err != nil {
		return out, planningInvalid("discovery JSON: %v", err)
	}
	if _, err := d.Token(); err != io.EOF {
		return out, ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&out); err != nil {
		return out, planningInvalid("discovery JSON: %v", err)
	}
	return out, nil
}
