package semanticreview

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// ApprovedEdit is a source-bound downstream compilation input, not an admitted
// task. The Apply adapter must prove qualification, read/validate mask and
// transform bytes, preserve protected regions and atomically admit the child.
type ApprovedEdit struct {
	SchemaVersion   int                           `json:"schema_version"`
	TargetID        uuid.UUID                     `json:"target_id"`
	Source          v2input.SessionVersionBinding `json:"source"`
	Intent          v2input.Ref                   `json:"intent"`
	Candidate       v2input.Ref                   `json:"candidate"`
	Mask            v2input.Ref                   `json:"mask"`
	Transform       v2input.Ref                   `json:"transform"`
	Kind            string                        `json:"kind"`
	Strength        float64                       `json:"strength"`
	EV              *float64                      `json:"ev"`
	Prompt          *string                       `json:"prompt"`
	PreservePersons bool                          `json:"preserve_persons"`
}

// CompileApprovedEdits preserves the exact semantic kind and transferred mask;
// it does not convert arbitrary masked edits into global/person-only overrides.
// No adapter availability or successful execution is implied by compilation.
func CompileApprovedEdits(prepared PreparedReview, targets []LockedTarget, request ApplyDecision) ([]ApprovedEdit, error) {
	proof, err := BuildDecisionProof(prepared, targets, request)
	if err != nil {
		return nil, err
	}
	owned, err := PrepareReviewDocuments(prepared.Sources, prepared.documents, prepared.modelSHA, prepared.datasetSHA)
	if err != nil {
		return nil, err
	}
	candidates := make(map[uuid.UUID]v2input.SemanticCandidate)
	refs := make(map[uuid.UUID]v2input.Ref)
	for _, document := range owned.documents {
		if document.Source.Contract != "TransferCandidate" {
			continue
		}
		candidate, err := v2input.DecodeSemanticCandidate(document.Body)
		if err != nil {
			return nil, ErrInvalid
		}
		candidates[document.Source.Ref.ArtifactID] = candidate
		refs[document.Source.Ref.ArtifactID] = document.Source.Ref
	}
	byID := make(map[uuid.UUID]LockedTarget, len(targets))
	for _, target := range targets {
		byID[target.ID] = target
	}
	result := make([]ApprovedEdit, 0, len(proof.Approved))
	for _, id := range proof.Approved {
		target := byID[id]
		candidate := candidates[*target.CandidateArtifactID]
		if candidate.State != "proposed" || candidate.Mask == nil || candidate.Transform == nil || candidate.Target.AssetVersionID != target.AssetVersionID {
			return nil, ErrConflict
		}
		edit := ApprovedEdit{SchemaVersion: 1, TargetID: id, Source: candidate.Target, Candidate: refs[*target.CandidateArtifactID], Intent: owned.IntentRef, Mask: *candidate.Mask, Transform: *candidate.Transform, Kind: owned.Intent.Kind, Strength: owned.Intent.Strength, PreservePersons: owned.Intent.PreservePersons}
		if owned.Intent.EV != nil {
			value := *owned.Intent.EV
			edit.EV = &value
		}
		if owned.Intent.Prompt != nil {
			value := *owned.Intent.Prompt
			edit.Prompt = &value
		}
		result = append(result, edit)
	}
	return result, nil
}
