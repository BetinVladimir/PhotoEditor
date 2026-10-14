package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type nativeInputManualSelection struct {
	tenant, project, actor, session, parent uuid.UUID
	binding                                 v2input.SessionVersionBinding
	policy                                  review.DefectProposalInputPolicy
	manuals                                 []review.OriginalManualProtection
	proofs                                  []defectManualExecutionProof
	pins                                    []nativeDefectInputPin
	params                                  PreparedDefectParams
	paramsRef                               v2input.Ref
	sourceBody                              string
	landmarks                               completedNativeLandmarks
	skin                                    completedNativeSkin
	producerSeals                           [4]string
}

// Preflight only. Retains independent original manual raster/vector evidence
// without substituting imported FAN/skin authority or relaxing native admission.
func (s SQLTransferSourceReader) readNativeInputSavedManualInputs(ctx context.Context, who auth.Identity, producers nativeInputRestoredFaceProducers, color TransferColorProfile) (nativeInputManualSelection, error) {
	var zero nativeInputManualSelection
	if err := verifyNativeInputFaceProducerPlans(ctx, who, producers); err != nil {
		return zero, err
	}
	d := producers.detector
	policy, err := s.readNativeInputSavedDefectPolicy(ctx, who, d.request, d.published.Ref, d.anchor.snapshotRef)
	if err != nil {
		return zero, err
	}
	landmarks, err := s.readCompletedNativeLandmarks(ctx, who, producers.landmarks, color)
	if err != nil {
		return zero, err
	}
	skin, err := s.readCompletedNativeSkin(ctx, who, producers.skin, color)
	if err != nil {
		return zero, err
	}
	expected, err := bindNativeDefectFaceInputs(ctx, landmarks.faces, review.ArtifactRef{ID: landmarks.catalog.Outputs[0].ArtifactID, SHA256: landmarks.catalog.Outputs[0].SHA256}, skin.mask, skin.parser.labels.Image)
	if err != nil {
		return zero, err
	}
	if err := verifyNativeInputSavedFaceBindings(ctx, policy.Faces, expected); err != nil {
		return zero, err
	}
	selection := nativeInputManualSelection{tenant: d.request.TenantID, project: s.ProjectID, actor: who.UserID, session: who.SessionID, parent: d.request.RunID, binding: d.request.Intent.Reference, policy: policy, manuals: make([]review.OriginalManualProtection, 0), proofs: make([]defectManualExecutionProof, 0)}
	seen := map[v2input.Ref]defectManualExecutionProof{}
	var used int
	for _, face := range policy.Faces {
		if face.Manual == nil {
			continue
		}
		original, proof, err := s.readManualPreparationProof(ctx, who, d.request.TenantID, d.request.Intent.Reference, *face.Manual)
		if err != nil {
			return zero, err
		}
		owned := ownDefectManualExecutionProof(proof)
		if previous, exists := seen[proof.objects.Pixel.ref]; exists {
			if previous != owned {
				return zero, v2input.ErrConflict
			}
			continue
		}
		if len(original.Body) > (4<<20)-used {
			return zero, v2input.ErrInvalid
		}
		used += len(original.Body)
		seen[proof.objects.Pixel.ref] = owned
		selection.manuals = append(selection.manuals, original)
		selection.proofs = append(selection.proofs, owned)
	}
	for _, proof := range selection.proofs {
		if err := s.withManualRasterScope().recheckManualPreparationProof(ctx, who, d.request.TenantID, d.request.Intent.Reference, proof.original()); err != nil {
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
	after, err := s.readNativeInputSavedDefectPolicy(ctx, who, d.request, d.published.Ref, d.anchor.snapshotRef)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(policy, after) {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckNativePlanPolicy(ctx, who, d.request.TenantID, d.policy); err != nil {
		return zero, err
	}
	selection.pins, err = nativeInputManualInventoryPins(ctx, selection, s.Bucket)
	if err != nil {
		return zero, err
	}
	imageRef := d.request.Intent.Reference.Source
	compiled, err := review.BuildDefectProposalTaskInputsFromImage(ctx, review.ArtifactRef{ID: imageRef.ArtifactID, SHA256: imageRef.SHA256}, []byte(landmarks.detector.sourceBody), policy.Faces, policy.Diameter, policy.Permanent, policy.Protections, selection.manuals)
	if err != nil {
		return zero, err
	}
	params, err := freezeCompiledDefectParams(ctx, compiled)
	if err != nil {
		return zero, err
	}
	saved, raw, err := s.readNativeInputDefectSavedParams(ctx, who, d.request, d.published.Ref, d.anchor.snapshotRef)
	if err != nil {
		return zero, err
	}
	if err := verifyNativeInputManualParamsReplay(ctx, params, saved.params, raw); err != nil {
		return zero, err
	}
	selection.params = params
	selection.paramsRef = saved.params
	selection.sourceBody = landmarks.detector.sourceBody
	selection.landmarks = landmarks
	selection.skin = skin
	selection.producerSeals = [4]string{producers.detector.seal, producers.landmarks.seal, producers.parser.seal, producers.skin.seal}
	if err := verifyNativeInputManualProducerBindings(ctx, who, producers, selection); err != nil {
		return zero, err
	}
	return selection, nil
}
