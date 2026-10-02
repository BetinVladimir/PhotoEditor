package v2learning

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TrainingModelLockSource struct {
	DB *pgxpool.Pool // photo_scheduler
}

type TrainingModelLock struct {
	ModelSHA   string
	DatasetSHA string
}

func (s TrainingModelLockSource) Load(ctx context.Context,
	start TrainingStart) (TrainingModelLock, error) {
	var found TrainingModelLock
	if s.DB == nil || start.TenantID == uuid.Nil ||
		start.ProjectID == uuid.Nil || start.ActorID == uuid.Nil ||
		start.AuthSessionID == uuid.Nil ||
		!validManifestRef(start.Request.Policy.Calibration) {
		return found, ErrInvalidTrainingDraft
	}
	err := s.DB.QueryRow(ctx, `SELECT model_lock_sha,dataset_sha
	 FROM photo.v23_training_model_lock($1,$2,$3,$4,$5)`,
		start.TenantID, start.ProjectID,
		start.Request.Policy.Calibration.SHA256, start.ActorID,
		start.AuthSessionID).Scan(&found.ModelSHA, &found.DatasetSHA)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrainingModelLock{}, ErrTrainingDraftConflict
	}
	if err != nil {
		return TrainingModelLock{}, err
	}
	if !validSHA(found.ModelSHA) || !validSHA(found.DatasetSHA) {
		return TrainingModelLock{}, ErrInvalidTrainingDraft
	}
	return found, nil
}
