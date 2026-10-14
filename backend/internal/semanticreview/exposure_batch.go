package semanticreview

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// ExposureTaskInputs must be resolved from hash-verified source/reference and
// mask documents under the authenticated review scope. Source and Reference
// bind this projection to the private review evidence, not mutable API fields.
type ExposureTaskInputs struct {
	TargetID          uuid.UUID
	Source, Reference v2input.SessionVersionBinding
	Width, Height     int64
	Dependencies      v2input.ExposureDependencies
	OwnerCosts        ExposureOwnerCosts
	Limits            ExposureResourceEstimate
	Qualification     ExposureQualificationPin
}

// CompileApprovedExposureTasks rebuilds every approved Params from private
// review evidence and requires exactly one resolved input for every approval.
// One invalid/unsupported target fails the entire compilation without returning
// partial tasks. Rejections create no task or resource reservation. The caller
// still must perform live qualification and atomic child admission in SQL.
func CompileApprovedExposureTasks(child uuid.UUID, prepared PreparedReview, targets []LockedTarget,
	request ApplyDecision, inputs []ExposureTaskInputs) ([]FrozenExposureTask, error) {
	if child == uuid.Nil || child == prepared.Sources.RunID {
		return nil, ErrInvalid
	}
	drafts, err := CompileApprovedTaskDrafts(child, prepared, targets, request)
	if err != nil {
		return nil, err
	}
	if len(inputs) != len(drafts) {
		return nil, ErrInvalid
	}
	owned, err := PrepareReviewDocuments(prepared.Sources, prepared.documents, prepared.modelSHA, prepared.datasetSHA)
	if err != nil {
		return nil, err
	}
	byTarget := make(map[uuid.UUID]ExposureTaskInputs, len(inputs))
	for _, input := range inputs {
		if input.TargetID == uuid.Nil {
			return nil, ErrInvalid
		}
		if _, duplicate := byTarget[input.TargetID]; duplicate {
			return nil, ErrInvalid
		}
		byTarget[input.TargetID] = input
	}
	result := make([]FrozenExposureTask, 0, len(drafts))
	for _, draft := range drafts {
		input, exists := byTarget[draft.TargetID]
		var edit ApprovedEdit
		if !exists || decodeStrict(draft.Body, &edit) != nil || edit.Source != input.Source ||
			input.Reference != owned.Intent.Reference || input.Dependencies.Reference != input.Reference.Source {
			return nil, ErrInvalid
		}
		task, err := CompileExposureTask(child, draft, input.Dependencies, input.Width, input.Height, input.OwnerCosts, input.Limits, input.Qualification)
		if err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	return result, nil
}
