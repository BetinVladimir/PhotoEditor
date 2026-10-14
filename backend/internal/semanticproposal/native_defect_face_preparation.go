package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type nativeDefectFacePreparation struct {
	landmarksPlan nativeFaceDraftPlan
	skinPlan      nativeSkinDraftPlan
	landmarks     completedNativeLandmarks
	skin          completedNativeSkin
	params        PreparedDefectParams
	person        *nativePersonInputAuthority
}

// Prepares original defect inputs from byte-verified native FAN and skin results.
// Both producers must share the exact original source/detector. Qualification,
// protected-mask preparation, publication and parent admission remain separate.
func (s SQLTransferSourceReader) prepareNativeDefectFaceParams(ctx context.Context, who auth.Identity, landmarksPlan nativeFaceDraftPlan, skinPlan nativeSkinDraftPlan, color TransferColorProfile, diameter [2]float64, permanent *review.ProtectionMask, additional []review.ProtectionMask, manuals []review.OriginalManualProtection) (nativeDefectFacePreparation, error) {
	var zero nativeDefectFacePreparation
	if err := verifyNativeFacePlanSeal(ctx, landmarksPlan); err != nil {
		return zero, err
	}
	if err := verifyNativeSkinPlanSeal(ctx, skinPlan); err != nil {
		return zero, err
	}
	if landmarksPlan.prepared.operation != "face.landmarks" || landmarksPlan.detector.seal != skinPlan.parser.detector.seal {
		return zero, v2input.ErrConflict
	}
	landmarks, err := s.readCompletedNativeLandmarks(ctx, who, landmarksPlan, color)
	if err != nil {
		return zero, err
	}
	skin, err := s.readCompletedNativeSkin(ctx, who, skinPlan, color)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(landmarks.detector, skin.parser.detector) || len(landmarks.faces) > 64 || skin.mask.ObjectID != nil || skin.mask.Protected {
		return zero, v2input.ErrConflict
	}
	faces, err := bindNativeDefectFaceInputs(ctx, landmarks.faces, review.ArtifactRef{ID: landmarks.catalog.Outputs[0].ArtifactID, SHA256: landmarks.catalog.Outputs[0].SHA256}, skin.mask, skin.parser.labels.Image)
	if err != nil {
		return zero, err
	}
	source := landmarksPlan.detector.request.Intent.Reference.Source
	params, err := PrepareDefectParams(ctx, review.ArtifactRef{ID: source.ArtifactID, SHA256: source.SHA256}, []byte(landmarks.detector.sourceBody), faces, diameter, permanent, additional, manuals)
	if err != nil {
		return zero, err
	}
	currentLandmarks, err := s.readCompletedNativeLandmarks(ctx, who, landmarksPlan, color)
	if err != nil {
		return zero, err
	}
	currentSkin, err := s.readCompletedNativeSkin(ctx, who, skinPlan, color)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(currentLandmarks, landmarks) || !reflect.DeepEqual(currentSkin, skin) {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativeDefectFacePreparation{landmarksPlan: landmarksPlan, skinPlan: skinPlan, landmarks: landmarks, skin: skin, params: params}, nil
}

// Binds each original face to the source-wide mask without sharing mutable
// geometry/model pointers with retained producer evidence. Not SQL authority.
func bindNativeDefectFaceInputs(ctx context.Context, faces []review.Face68, landmarks review.ArtifactRef, skin review.ProtectionMask, image review.ArtifactRef) ([]review.DefectProposalFaceInput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if faces == nil || len(faces) > 64 || landmarks.ID == uuid.Nil || skin.Artifact.ID == uuid.Nil || landmarks.ID == skin.Artifact.ID || skin.ObjectID != nil || skin.Protected || skin.Selector != "skin" || skin.ModelLockID == nil || math.IsNaN(skin.Confidence) || skin.Confidence < 0 || skin.Confidence > 1 {
		return nil, v2input.ErrInvalid
	}
	for _, digest := range []string{landmarks.SHA256, skin.Artifact.SHA256, *skin.ModelLockID} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return nil, v2input.ErrInvalid
		}
	}
	source := review.DefectSource{Image: image, Canvas: skin.Canvas, TransformChain: skin.TransformChain}
	result := make([]review.DefectProposalFaceInput, len(faces))
	seen := map[uuid.UUID]bool{}
	for i, face := range faces {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if seen[face.ObjectID] {
			return nil, v2input.ErrConflict
		}
		raw, err := canonical.Marshal(face)
		if err != nil {
			return nil, err
		}
		if _, err := review.ParseFace68(raw, source); err != nil {
			return nil, v2input.ErrConflict
		}
		seen[face.ObjectID] = true
		object := face.ObjectID
		model := *skin.ModelLockID
		mask := skin
		mask.ObjectID = &object
		mask.ModelLockID = &model
		mask.TransformChain = append([]byte(nil), skin.TransformChain...)
		result[i] = review.DefectProposalFaceInput{ObjectID: &object, Landmarks: landmarks, Skin: &mask}
	}
	return result, nil
}
