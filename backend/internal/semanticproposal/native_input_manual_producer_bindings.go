package semanticproposal

import (
	"context"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Verifies retained physical FAN/skin observations against the original private
// producer chain and selected manual face declarations. No SQL or object IO;
// caller still needs producer/result locks before committing any graph change.
func verifyNativeInputManualProducerBindings(ctx context.Context, who auth.Identity, producers nativeInputRestoredFaceProducers, selection nativeInputManualSelection) error {
	if err := verifyNativeInputFaceProducerPlans(ctx, who, producers); err != nil {
		return err
	}
	d := producers.detector
	if selection.producerSeals != ([4]string{d.seal, producers.landmarks.seal, producers.parser.seal, producers.skin.seal}) {
		return v2input.ErrConflict
	}
	if selection.tenant != d.request.TenantID || selection.actor != who.UserID || selection.session != who.SessionID || selection.parent != d.request.RunID || selection.binding != d.request.Intent.Reference || selection.project != d.shell.Project {
		return v2input.ErrConflict
	}
	landmarks, skin := selection.landmarks, selection.skin
	if len(landmarks.catalog.Outputs) != 1 || selection.sourceBody != landmarks.detector.sourceBody || !reflect.DeepEqual(landmarks.detector, skin.parser.detector) {
		return v2input.ErrConflict
	}
	expected, err := bindNativeDefectFaceInputs(ctx, landmarks.faces, review.ArtifactRef{ID: landmarks.catalog.Outputs[0].ArtifactID, SHA256: landmarks.catalog.Outputs[0].SHA256}, skin.mask, skin.parser.labels.Image)
	if err != nil {
		return err
	}
	if err := verifyNativeInputSavedFaceBindings(ctx, selection.policy.Faces, expected); err != nil {
		return err
	}
	return verifyNativeInputManualSelectionParams(ctx, selection)
}
