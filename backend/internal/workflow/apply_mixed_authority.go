package workflow

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2qualification"
)

func qualifyApplyMixedProfile(config json.RawMessage, tenant uuid.UUID, operation string, profile json.RawMessage) (json.RawMessage, error) {
	var doc map[string]json.RawMessage
	if json.Unmarshal(config, &doc) != nil {
		return nil, v2qualification.ErrUnqualified
	}
	var p struct {
		Backend string `json:"backend"`
	}
	if json.Unmarshal(profile, &p) != nil {
		return nil, v2qualification.ErrUnqualified
	}
	raw, found := doc["mixed_renderer_authority"]
	if !found {
		if p.Backend == "mps" {
			return nil, v2qualification.ErrUnqualified
		}
		return profile, nil
	}
	var scope struct {
		Project uuid.UUID `json:"project_id"`
	}
	if json.Unmarshal(raw, &scope) != nil {
		return nil, v2qualification.ErrUnqualified
	}
	a, e := v2qualification.DecodeMixedAuthority(raw, tenant, scope.Project)
	if e != nil {
		return nil, e
	}
	return a.QualifyProfile(operation, "1", profile)
}

// Only the concrete isolated operator ledger can use child-only measurement
// authority. SQL-backed Apply stores always retain the accepted-parent gate.
func (c ApplyCoordinator) qualifyMixedProfile(config json.RawMessage, tenant uuid.UUID, operation string, profile json.RawMessage) (json.RawMessage, error) {
	if ledger, ok := c.Store.(*OperatorApplyLedger); ok && ledger.mixedChildren != nil {
		return ledger.mixedChildren.QualifyProfile(tenant, ledger.lease.RunID, ledger.mixedCorpus, operation, profile)
	}
	return qualifyApplyMixedProfile(config, tenant, operation, profile)
}
