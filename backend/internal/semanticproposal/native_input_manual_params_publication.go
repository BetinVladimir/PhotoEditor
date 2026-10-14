package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type nativeInputPublishedManualParams struct{ selection nativeInputManualSelection }

// Accepted-version publication only. Original manual/native evidence is read
// before and after object IO; graph ownership/qualification/admission are separate.
func (s SQLTransferSourceReader) publishNativeInputManualInitialParams(ctx context.Context, who auth.Identity, producers nativeInputRestoredFaceProducers, initial nativeInputManualInitialPreparation, color TransferColorProfile, publisher v2draft.Publisher) (nativeInputPublishedManualParams, error) {
	var zero nativeInputPublishedManualParams
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	selection := initial.selection
	if selection.paramsRef != (v2input.Ref{}) {
		return zero, v2input.ErrInvalid
	}
	if err := verifyNativeInputFaceProducerPlans(ctx, who, producers); err != nil {
		return zero, err
	}
	document := uuid.NewSHA1(producers.detector.request.RunID, []byte("semantic.defect.params:reference"))
	// A local schema check ref is never passed to the publisher as an artifact.
	check := selection
	check.paramsRef = v2input.Ref{ArtifactID: document, SHA256: selection.params.digest}
	if err := verifyNativeInputManualProducerBindings(ctx, who, producers, check); err != nil {
		return zero, err
	}
	recheck := func() error {
		current, err := s.prepareNativeInputManualInitialParams(ctx, who, producers, color, selection.policy.Faces, selection.policy.Diameter)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, initial) {
			return v2input.ErrConflict
		}
		return ctx.Err()
	}
	if err := recheck(); err != nil {
		return zero, err
	}
	var inputs, roots []review.ArtifactRef
	if json.Unmarshal([]byte(selection.params.inputs), &inputs) != nil || json.Unmarshal([]byte(selection.params.roots), &roots) != nil {
		return zero, v2input.ErrInvalid
	}
	for _, input := range inputs {
		if input.ID == document {
			return zero, v2input.ErrConflict
		}
	}
	ref, err := publisher.PublishDefectProposalParams(ctx, who, selection.tenant, document, selection.binding, []byte(selection.sourceBody), []byte(selection.params.params), inputs)
	if err != nil {
		return zero, err
	}
	if ref.ArtifactID == uuid.Nil || ref.SHA256 != selection.params.digest {
		return zero, v2input.ErrConflict
	}
	for _, input := range inputs {
		if input.ID == ref.ArtifactID {
			return zero, v2input.ErrConflict
		}
	}
	if err := recheck(); err != nil {
		return zero, err
	}
	selection.paramsRef = ref
	if err := verifyNativeInputManualProducerBindings(ctx, who, producers, selection); err != nil {
		return zero, err
	}
	return nativeInputPublishedManualParams{selection: selection}, nil
}
