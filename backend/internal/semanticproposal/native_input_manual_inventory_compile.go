package semanticproposal

import (
	"context"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Compiles only manual-root-backed inventory; independently protected masks
// require separate authority and cannot be injected as additional caller pins.
func (s SQLTransferSourceReader) compileNativeInputManualInventory(ctx context.Context, who auth.Identity, dependencies nativeInputManualDependencies, params defectEvidenceObject) ([]v2input.Entry, []v2input.SemanticInputSize, error) {
	if err := verifyNativeInputManualProducerBindings(ctx, who, dependencies.producers, dependencies.selection); err != nil {
		return nil, nil, err
	}
	digest, err := s.nativeInputManualDependencyDigest(ctx, dependencies)
	if err != nil {
		return nil, nil, err
	}
	if dependencies.seal == "" || digest != dependencies.seal {
		return nil, nil, v2input.ErrConflict
	}
	if err := s.verifyNativeInputManualCompletedPackets(ctx, dependencies); err != nil {
		return nil, nil, err
	}
	selection := dependencies.selection
	if len(selection.proofs) < 1 || len(selection.proofs) > 64 {
		return nil, nil, v2input.ErrInvalid
	}
	if params.ref != selection.paramsRef {
		return nil, nil, v2input.ErrConflict
	}
	pins, err := nativeInputManualInventoryPins(ctx, selection, s.Bucket)
	if err != nil {
		return nil, nil, err
	}
	if !reflect.DeepEqual(pins, selection.pins) {
		return nil, nil, v2input.ErrConflict
	}
	// This is data for pure assembly, never a bare-face admission certificate.
	data := nativeDefectFacePreparation{landmarksPlan: dependencies.producers.landmarks, skinPlan: dependencies.producers.skin, landmarks: selection.landmarks, skin: selection.skin, params: selection.params}
	return assembleNativeDefectInventory(ctx, data, params, pins)
}
