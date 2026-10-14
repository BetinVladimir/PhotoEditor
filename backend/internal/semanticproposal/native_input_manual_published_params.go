package semanticproposal

import (
	"context"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type nativeInputPublishedManualParamsProof struct {
	published nativeInputPublishedManualParams
	params    defectEvidenceObject
}

// Reads accepted-version Params without requiring an existing defect draft.
// The accepted catalog pin and original compiler bytes must match before and
// after independent physical manual/native producer rechecks.
func (s SQLTransferSourceReader) readNativeInputPublishedManualParams(ctx context.Context, who auth.Identity, producers nativeInputRestoredFaceProducers, published nativeInputPublishedManualParams, color TransferColorProfile) (nativeInputPublishedManualParamsProof, error) {
	var zero nativeInputPublishedManualParamsProof
	selection := published.selection
	if err := verifyNativeInputManualProducerBindings(ctx, who, producers, selection); err != nil {
		return zero, err
	}
	recheck := func() error {
		current, err := s.prepareNativeInputManualInitialParams(ctx, who, producers, color, selection.policy.Faces, selection.policy.Diameter)
		if err != nil {
			return err
		}
		current.selection.paramsRef = selection.paramsRef
		if !reflect.DeepEqual(current.selection, selection) {
			return v2input.ErrConflict
		}
		return ctx.Err()
	}
	if err := recheck(); err != nil {
		return zero, err
	}
	document := uuid.NewSHA1(selection.parent, []byte("semantic.defect.params:reference"))
	object, err := s.defectVersionParamsObject(ctx, who, selection.tenant, selection.binding, document, selection.paramsRef)
	if err != nil {
		return zero, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, selection.paramsRef)
	if err != nil {
		return zero, err
	}
	if err := verifyNativeInputManualParamsReplay(ctx, selection.params, selection.paramsRef, raw); err != nil {
		return zero, err
	}
	if err := recheck(); err != nil {
		return zero, err
	}
	current, err := s.defectVersionParamsObject(ctx, who, selection.tenant, selection.binding, document, selection.paramsRef)
	if err != nil {
		return zero, err
	}
	if current != object {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativeInputPublishedManualParamsProof{published: published, params: defectEvidenceObject{ref: selection.paramsRef, object: object}}, nil
}
