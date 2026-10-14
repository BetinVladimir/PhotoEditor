package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Unpublished compiler inputs; paramsRef remains empty and cannot attest an
// accepted artifact or enter the saved/manual admission path.
type nativeInputManualInitialPreparation struct{ selection nativeInputManualSelection }

// Faces are internal server-selected locators, not producer authority. Every
// manual target is independently resolved from its original raster/vector task.
// Permanent/additional source protection has its separate preparation path.
func (s SQLTransferSourceReader) prepareNativeInputManualInitialParams(ctx context.Context, who auth.Identity, producers nativeInputRestoredFaceProducers, color TransferColorProfile, faces []review.DefectProposalFaceInput, diameter [2]float64) (nativeInputManualInitialPreparation, error) {
	var zero nativeInputManualInitialPreparation
	if err := verifyNativeInputFaceProducerPlans(ctx, who, producers); err != nil {
		return zero, err
	}
	if faces == nil || len(faces) > 64 {
		return zero, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(faces)
	if err != nil || len(raw) > 1<<20 {
		return zero, v2input.ErrInvalid
	}
	var owned []review.DefectProposalFaceInput
	if json.Unmarshal(raw, &owned) != nil {
		return zero, v2input.ErrInvalid
	}
	landmarks, err := s.readCompletedNativeLandmarks(ctx, who, producers.landmarks, color)
	if err != nil {
		return zero, err
	}
	skin, err := s.readCompletedNativeSkin(ctx, who, producers.skin, color)
	if err != nil {
		return zero, err
	}
	if len(landmarks.catalog.Outputs) != 1 || !reflect.DeepEqual(landmarks.detector, skin.parser.detector) {
		return zero, v2input.ErrConflict
	}
	expected, err := bindNativeDefectFaceInputs(ctx, landmarks.faces, review.ArtifactRef{ID: landmarks.catalog.Outputs[0].ArtifactID, SHA256: landmarks.catalog.Outputs[0].SHA256}, skin.mask, skin.parser.labels.Image)
	if err != nil {
		return zero, err
	}
	if err := verifyNativeInputSavedFaceBindings(ctx, owned, expected); err != nil {
		return zero, err
	}
	d := producers.detector
	selection := nativeInputManualSelection{tenant: d.request.TenantID, project: s.ProjectID, actor: who.UserID, session: who.SessionID, parent: d.request.RunID, binding: d.request.Intent.Reference, sourceBody: landmarks.detector.sourceBody, landmarks: landmarks, skin: skin, producerSeals: [4]string{d.seal, producers.landmarks.seal, producers.parser.seal, producers.skin.seal}, manuals: make([]review.OriginalManualProtection, 0), proofs: make([]defectManualExecutionProof, 0)}
	seen := map[v2input.Ref]defectManualExecutionProof{}
	used := 0
	for _, face := range owned {
		if face.Manual == nil {
			continue
		}
		original, proof, err := s.readManualPreparationProof(ctx, who, selection.tenant, selection.binding, *face.Manual)
		if err != nil {
			return zero, err
		}
		ownedProof := ownDefectManualExecutionProof(proof)
		if old, exists := seen[proof.objects.Pixel.ref]; exists {
			if old != ownedProof {
				return zero, v2input.ErrConflict
			}
			continue
		}
		if len(original.Body) > (4<<20)-used {
			return zero, v2input.ErrInvalid
		}
		used += len(original.Body)
		seen[proof.objects.Pixel.ref] = ownedProof
		selection.manuals = append(selection.manuals, original)
		selection.proofs = append(selection.proofs, ownedProof)
	}
	if len(selection.proofs) == 0 {
		return zero, v2input.ErrInvalid
	}
	compiled, err := review.BuildDefectProposalTaskInputsFromImage(ctx, review.ArtifactRef{ID: selection.binding.Source.ArtifactID, SHA256: selection.binding.Source.SHA256}, []byte(selection.sourceBody), owned, diameter, nil, nil, selection.manuals)
	if err != nil {
		return zero, err
	}
	selection.params, err = freezeCompiledDefectParams(ctx, compiled)
	if err != nil {
		return zero, err
	}
	image, err := review.ParseWorkingImage([]byte(selection.sourceBody))
	if err != nil {
		return zero, err
	}
	// Local schema-verification identity only; never retained as an accepted ref.
	localRef := review.ArtifactRef{ID: uuid.NewSHA1(d.request.RunID, []byte("semantic.defect.params:reference")), SHA256: selection.params.digest}
	selection.policy, err = review.DefectProposalInputPolicyFromParams(localRef, compiled.Params, image.Source(review.ArtifactRef{ID: selection.binding.Source.ArtifactID, SHA256: selection.binding.Source.SHA256}))
	if err != nil {
		return zero, err
	}
	selection.pins, err = nativeInputManualInventoryPins(ctx, selection, s.Bucket)
	if err != nil {
		return zero, err
	}
	for _, proof := range selection.proofs {
		if err := s.withManualRasterScope().recheckManualPreparationProof(ctx, who, selection.tenant, selection.binding, proof.original()); err != nil {
			return zero, err
		}
	}
	currentLandmarks, err := s.readCompletedNativeLandmarks(ctx, who, producers.landmarks, color)
	if err != nil {
		return zero, err
	}
	currentSkin, err := s.readCompletedNativeSkin(ctx, who, producers.skin, color)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(landmarks, currentLandmarks) || !reflect.DeepEqual(skin, currentSkin) {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckNativePlanPolicy(ctx, who, selection.tenant, d.policy); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativeInputManualInitialPreparation{selection: selection}, nil
}
