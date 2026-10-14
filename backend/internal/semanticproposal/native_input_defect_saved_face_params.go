package semanticproposal

import (
	"context"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Recompiles from physically verified original producers and compares the saved
// Params without publishing replacements. Producer-plan loading and subsequent
// defect qualification/admission remain separate recovery steps.
func (s SQLTransferSourceReader) restoreNativeInputDefectFaceParams(ctx context.Context, who auth.Identity, request Request, intent, snapshot v2input.Ref, landmarks nativeFaceDraftPlan, skin nativeSkinDraftPlan, color TransferColorProfile, diameter [2]float64, permanent *review.ProtectionMask, additional []review.ProtectionMask, manuals []review.OriginalManualProtection) (nativeDefectFacePreparation, error) {
	var zero nativeDefectFacePreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := verifyNativeFacePlanSeal(ctx, landmarks); err != nil {
		return zero, err
	}
	if err := verifyNativeSkinPlanSeal(ctx, skin); err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(landmarks.detector.request, request) || !reflect.DeepEqual(skin.parser.detector.request, request) || landmarks.detector.published.Ref != intent || skin.parser.detector.published.Ref != intent {
		return zero, v2input.ErrConflict
	}
	saved, raw, err := s.readNativeInputDefectSavedParams(ctx, who, request, intent, snapshot)
	if err != nil {
		return zero, err
	}
	if len(landmarks.shell.Tasks) != 1 || len(skin.shell.Tasks) != 1 || landmarks.shell.Run != saved.landmarksRun || landmarks.shell.Tasks[0].ID != saved.landmarksTask || skin.shell.Run != saved.skinRun || skin.shell.Tasks[0].ID != saved.skinTask {
		return zero, v2input.ErrConflict
	}
	var prepared nativeDefectFacePreparation
	if saved.person != nil {
		if len(additional) != 0 || len(manuals) != 0 {
			return zero, v2input.ErrConflict
		}
		person, restoreErr := s.restoreNativeDefectPersonInput(ctx, who, request, *saved.person)
		if restoreErr != nil {
			return zero, restoreErr
		}
		prepared, err = s.prepareNativeDefectPersonFaceParams(ctx, who, landmarks, skin, color, diameter, person)
		if err == nil && permanent != nil {
			mask, projectionErr := projectNativePersonProtectionInput(ctx, person, []byte(prepared.landmarks.detector.sourceBody))
			if projectionErr != nil {
				return zero, projectionErr
			}
			if !reflect.DeepEqual(*permanent, mask) {
				return zero, v2input.ErrConflict
			}
		}
	} else {
		prepared, err = s.prepareNativeDefectFaceParams(ctx, who, landmarks, skin, color, diameter, permanent, additional, manuals)
	}
	if err != nil {
		return zero, err
	}
	if err := verifyNativeDefectPreparedParams(ctx, prepared); err != nil {
		return zero, err
	}
	if prepared.params.digest != saved.params.SHA256 || prepared.params.params != string(raw) {
		return zero, v2input.ErrConflict
	}
	// Producer IO must not leave an earlier saved config/object observation live.
	after, current, err := s.readNativeInputDefectSavedParams(ctx, who, request, intent, snapshot)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(after, saved) || string(current) != string(raw) {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return prepared, nil
}
