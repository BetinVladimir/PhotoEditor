package v2learning

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type TrainingModelLockReader interface {
	Load(context.Context, TrainingStart) (TrainingModelLock, error)
}

type TrainingCalibrationReader interface {
	Load(context.Context, TrainingStart, string) (TrainingCalibration, error)
}

type TrainingBaselineReader interface {
	LoadActive(context.Context, TrainingStart) (*TrainingActiveBaseline, error)
}

// TrainingContextSource resolves locks only for a missing draft. On pending
// replay, callers must use the immutable context already in recovery manifest.
type TrainingContextSource struct {
	Model       TrainingModelLockReader
	Calibration TrainingCalibrationReader
	Baseline    TrainingBaselineReader
}

func (s TrainingContextSource) Load(ctx context.Context,
	start TrainingStart, inventory TrainingInventory, iccLock string) (
	ManifestContext, error) {
	var empty ManifestContext
	if _, err := ValidateTrainCreate(start.Request); err != nil {
		return empty, ErrInvalidTrainingDraft
	}
	if s.Model == nil || s.Calibration == nil || s.Baseline == nil ||
		start.TenantID == uuid.Nil || start.ActorID == uuid.Nil ||
		start.AuthSessionID == uuid.Nil ||
		start.ProfileID == uuid.Nil || start.ProjectID == uuid.Nil ||
		inventory.ProjectID != start.ProjectID ||
		inventory.Mode != start.Request.Policy.Mode ||
		(inventory.Mode == "person") != (inventory.PersonID != nil) ||
		!validSHA(iccLock) {
		return empty, ErrInvalidTrainingDraft
	}
	locks, err := s.Model.Load(ctx, start)
	if err != nil {
		return empty, err
	}
	if !validSHA(locks.ModelSHA) || !validSHA(locks.DatasetSHA) {
		return empty, ErrInvalidTrainingDraft
	}
	calibration, err := s.Calibration.Load(ctx, start, locks.ModelSHA)
	if err != nil {
		return empty, err
	}
	if calibration.DatasetSHA != locks.DatasetSHA ||
		calibration.Ref != start.Request.Policy.Calibration {
		return empty, ErrTrainingDraftConflict
	}
	active, err := s.Baseline.LoadActive(ctx, start)
	if err != nil {
		return empty, err
	}
	var baseline v2input.Ref
	if active == nil {
		runID, err := v2input.RunID(start.TenantID, start.ActorID, start.Key)
		if err != nil {
			return empty, err
		}
		zero, err := BuildTrainingZeroBaseline(runID, start.ProfileID)
		if err != nil {
			return empty, err
		}
		baseline = zero.Ref
	} else {
		if active.Profile.ProfileID != start.ProfileID ||
			active.Profile.TrainingSHA != active.TrainingSHA ||
			!validManifestRef(active.Ref) {
			return empty, ErrInvalidTrainingDraft
		}
		baseline = active.Ref
	}
	policySHA, err := canonical.Hash(start.Request.Policy)
	if err != nil {
		return empty, ErrInvalidTrainingDraft
	}
	return ManifestContext{ProfileID: start.ProfileID,
		ProjectID: start.ProjectID, Mode: inventory.Mode,
		PersonID: inventory.PersonID, PolicySHA: policySHA,
		Baseline: baseline, ICCLockSHA: iccLock,
		ModelLockSHA: locks.ModelSHA, Calibration: calibration.Ref}, nil
}
