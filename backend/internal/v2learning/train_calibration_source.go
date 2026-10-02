package v2learning

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2sessions"
)

// TrainingCalibrationSource reads only the DBA-pinned calibration ref of a
// currently qualified model. The returned dataset lock comes from that same
// policy, never from the client-supplied JSON document.
type TrainingCalibrationSource struct {
	DB      *pgxpool.Pool // photo_scheduler
	Objects LearningEvaluateObjectReader
	Bucket  string
}

type TrainingCalibration struct {
	Ref        v2input.Ref
	DatasetSHA string
}

func (s TrainingCalibrationSource) Load(ctx context.Context, start TrainingStart,
	modelLock string) (TrainingCalibration, error) {
	var empty TrainingCalibration
	ref := start.Request.Policy.Calibration
	if s.DB == nil || s.Objects == nil || s.Bucket == "" ||
		!validManifestRef(ref) || !validSHA(modelLock) ||
		start.TenantID == uuid.Nil || start.ProjectID == uuid.Nil ||
		start.ActorID == uuid.Nil || start.AuthSessionID == uuid.Nil {
		return empty, ErrInvalidTrainingDraft
	}
	var datasetSHA, bucket string
	var object objectstore.Object
	err := s.DB.QueryRow(ctx, `SELECT dataset_sha,bucket,object_key,
	 object_version,bytes,media_type FROM photo.v23_training_calibration_source(
	 $1,$2,$3,$4,$5,$6,$7)`, start.TenantID, start.ProjectID,
		ref.ArtifactID, ref.SHA256, modelLock, start.ActorID,
		start.AuthSessionID).Scan(&datasetSHA, &bucket, &object.Key,
		&object.VersionID, &object.Bytes, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrTrainingDraftConflict
	}
	if err != nil {
		return empty, err
	}
	if bucket != s.Bucket || !validSHA(datasetSHA) ||
		object.Bytes > 1_048_576 {
		return empty, ErrInvalidTrainingDraft
	}
	object.SHA256 = ref.SHA256
	body, err := ReadFrozenRootDocument(ctx, s.Objects, object, ref)
	if err != nil {
		return empty, err
	}
	if err := v2sessions.VerifyCalibrationBytes(body, ref.SHA256,
		modelLock, datasetSHA); err != nil {
		return empty, err
	}
	return TrainingCalibration{Ref: ref, DatasetSHA: datasetSHA}, nil
}
