package semanticreview

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// InitialTarget is a persistence seed, before any human decision or child Apply.
// Tenant, project and intent IDs must come from the locked workflow repository.
type InitialTarget struct {
	AssetVersionID      uuid.UUID
	CandidateArtifactID uuid.UUID
	State               string
}

func CompileInitialTargets(planRef v2input.Ref, planRaw []byte, params v2input.SemanticAssembleParams, snapshotRef v2input.Ref, snapshotRaw []byte, documents []v2input.SemanticCandidateDocument) ([]InitialTarget, error) {
	plan, err := v2input.VerifySemanticPlan(planRef, planRaw, params, snapshotRef, snapshotRaw, documents)
	if err != nil {
		return nil, ErrInvalid
	}
	byVersion := make(map[uuid.UUID]uuid.UUID, len(documents))
	for _, document := range documents {
		candidate, err := v2input.DecodeSemanticCandidate(document.Body)
		if err != nil {
			return nil, ErrInvalid
		}
		byVersion[candidate.Target.AssetVersionID] = document.Ref.ArtifactID
	}
	rows := make([]InitialTarget, len(plan.Candidates))
	for n, candidate := range plan.Candidates {
		rows[n] = InitialTarget{candidate.Target.AssetVersionID, byVersion[candidate.Target.AssetVersionID], candidate.State}
	}
	return rows, nil
}
