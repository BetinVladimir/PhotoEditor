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
	"photoeditor/backend/internal/v2qualification"
)

// Bind server-selected manual target locators to original native faces. Original
// raster/vector/content verification remains mandatory before use. Multiple
// candidates for one face or unused/foreign candidates refuse, never pick newest.
func bindInitialManualFaceInputs(ctx context.Context, faces []review.DefectProposalFaceInput, targets []review.ProtectionMask) ([]review.DefectProposalFaceInput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if faces == nil || len(faces) > 64 || targets == nil || len(targets) < 1 || len(targets) > 64 {
		return nil, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(faces)
	if err != nil || len(raw) > 1<<20 {
		return nil, v2input.ErrInvalid
	}
	var owned []review.DefectProposalFaceInput
	if json.Unmarshal(raw, &owned) != nil {
		return nil, v2input.ErrInvalid
	}
	raw, err = canonical.Marshal(targets)
	if err != nil || len(raw) > 1<<20 {
		return nil, v2input.ErrInvalid
	}
	var masks []review.ProtectionMask
	if json.Unmarshal(raw, &masks) != nil {
		return nil, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, target := range masks {
		if target.Artifact.ID == uuid.Nil || seen[target.Artifact.ID] || target.Protected || target.Selector != "manual" {
			return nil, v2input.ErrConflict
		}
		seen[target.Artifact.ID] = true
	}
	used := make([]bool, len(masks))
	seenFaces := map[uuid.UUID]bool{}
	for i, face := range owned {
		if face.ObjectID == nil || *face.ObjectID == uuid.Nil || seenFaces[*face.ObjectID] || face.Manual != nil || face.ManualMode {
			return nil, v2input.ErrConflict
		}
		seenFaces[*face.ObjectID] = true
		selected := -1
		for j, target := range masks {
			if target.ObjectID != nil && *target.ObjectID != *face.ObjectID {
				continue
			}
			if selected != -1 {
				return nil, v2input.ErrConflict
			}
			selected = j
		}
		if selected >= 0 {
			mask := masks[selected]
			mask.TransformChain = append([]byte(nil), mask.TransformChain...)
			owned[i].Manual = &mask
			used[selected] = true
		}
	}
	for _, matched := range used {
		if !matched {
			return nil, v2input.ErrConflict
		}
	}
	return owned, ctx.Err()
}

// Selected target refs are locators only. Every original manual producer and
// binary target/protected raster is independently verified, with a shared bounded
// raster budget. This does not discover target refs from unrelated user masks.
func (s SQLTransferSourceReader) selectInitialManualFaceInputs(ctx context.Context, who auth.Identity, producers nativeInputRestoredFaceProducers, color TransferColorProfile, targets []review.ProtectionMask) ([]review.DefectProposalFaceInput, error) {
	if err := verifyNativeInputFaceProducerPlans(ctx, who, producers); err != nil {
		return nil, err
	}
	if targets == nil || len(targets) < 1 || len(targets) > 64 {
		return nil, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(targets)
	if err != nil || len(body) > 1<<20 {
		return nil, v2input.ErrInvalid
	}
	s = s.withManualRasterScope()
	landmarks, err := s.readCompletedNativeLandmarks(ctx, who, producers.landmarks, color)
	if err != nil {
		return nil, err
	}
	skin, err := s.readCompletedNativeSkin(ctx, who, producers.skin, color)
	if err != nil {
		return nil, err
	}
	if len(landmarks.catalog.Outputs) != 1 || !reflect.DeepEqual(landmarks.detector, skin.parser.detector) {
		return nil, v2input.ErrConflict
	}
	expected, err := bindNativeDefectFaceInputs(ctx, landmarks.faces, review.ArtifactRef{ID: landmarks.catalog.Outputs[0].ArtifactID, SHA256: landmarks.catalog.Outputs[0].SHA256}, skin.mask, skin.parser.labels.Image)
	if err != nil {
		return nil, err
	}
	selected, err := bindInitialManualFaceInputs(ctx, expected, targets)
	if err != nil {
		return nil, err
	}
	var proofs []manualPreparationProof
	seen := map[uuid.UUID]bool{}
	for _, face := range selected {
		if face.Manual == nil || seen[face.Manual.Artifact.ID] {
			continue
		}
		seen[face.Manual.Artifact.ID] = true
		_, proof, err := s.readManualPreparationProof(ctx, who, producers.detector.request.TenantID, producers.detector.request.Intent.Reference, *face.Manual)
		if err != nil {
			return nil, err
		}
		proofs = append(proofs, proof)
	}
	currentLandmarks, err := s.readCompletedNativeLandmarks(ctx, who, producers.landmarks, color)
	if err != nil {
		return nil, err
	}
	currentSkin, err := s.readCompletedNativeSkin(ctx, who, producers.skin, color)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(landmarks, currentLandmarks) || !reflect.DeepEqual(skin, currentSkin) {
		return nil, v2input.ErrConflict
	}
	for _, proof := range proofs {
		if err := s.recheckManualPreparationProof(ctx, who, producers.detector.request.TenantID, producers.detector.request.Intent.Reference, proof); err != nil {
			return nil, err
		}
	}
	if err := verifyNativeInputSavedFaceBindings(ctx, selected, expected); err != nil {
		return nil, err
	}
	return selected, ctx.Err()
}

// Complete manual-start assembly from server-selected mask locators; it cannot
// manufacture manual evidence or substitute a bare defect path on any failure.
func (b nativePreparationBootstrap) bootstrapSelectedQualifiedManualDefect(ctx context.Context, who auth.Identity, producers nativeInputRestoredFaceProducers, color TransferColorProfile, targets []review.ProtectionMask, diameter [2]float64, capacity TransferPreparation, reserved int64, documents v2input.DocumentPublisher) (nativeInputManualInitialSubmission, error) {
	if err := b.validate(ctx); err != nil {
		return nativeInputManualInitialSubmission{}, err
	}
	if documents == nil || capacity.Color.sha != color.sha {
		return nativeInputManualInitialSubmission{}, v2input.ErrInvalid
	}
	if _, err := initialNativeDefectBudget(capacity, reserved, v2qualification.Metrics{}); err != nil {
		return nativeInputManualInitialSubmission{}, err
	}
	b.Reader = b.Reader.withManualRasterScope()
	faces, err := b.Reader.selectInitialManualFaceInputs(ctx, who, producers, color, targets)
	if err != nil {
		return nativeInputManualInitialSubmission{}, err
	}
	return b.bootstrapQualifiedManualDefect(ctx, who, producers, color, faces, diameter, capacity, reserved, documents)
}
