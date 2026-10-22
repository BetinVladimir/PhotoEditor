package review

import (
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
)

// OperatorCullingScope describes isolated measurement provenance, not an SQL
// snapshot or authenticated review. The service continues to authorize/load
// its own live snapshot; this lower pure entry shares its exact decision rules.
type OperatorCullingScope struct {
	Tenant, Actor, Run, Review, Snapshot uuid.UUID
	SnapshotDigest                       string
	CandidateHash                        string
	Projection                           json.RawMessage
}
type OperatorCullingPlan struct {
	Selected     []uuid.UUID `json:"selected_item_ids"`
	Confirmed    []uuid.UUID `json:"confirmed_eyes_item_ids"`
	NeverDelete  bool        `json:"never_delete"`
	DecisionHash string      `json:"decision_hash"`
}

func PlanOperatorCulling(s OperatorCullingScope, request CullingDecision) (OperatorCullingPlan, error) {
	if _, e := domain.ParseDigest(s.SnapshotDigest); e != nil {
		return OperatorCullingPlan{}, errors.New("operator culling provenance digest invalid")
	}
	projection, e := readCullingProjection(s.Projection, s.CandidateHash)
	if e != nil {
		return OperatorCullingPlan{}, e
	}
	context := cullingContext{View: View{ID: s.Review, RunID: s.Run, Kind: "culling", State: "pending", Revision: 1, CandidateHash: s.CandidateHash}, Snapshot: s.Snapshot, Digest: s.SnapshotDigest, Projection: projection}
	selection, hash, e := planCullingDecision(s.Tenant, s.Actor, context, request)
	if e != nil {
		return OperatorCullingPlan{}, e
	}
	return OperatorCullingPlan{Selected: selection.Selected, Confirmed: selection.Confirmed, NeverDelete: selection.NeverDelete, DecisionHash: hash}, nil
}
