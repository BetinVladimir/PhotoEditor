package v2sessions

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2calibration"
)

type ObjectReader interface {
	Open(context.Context, objectstore.Object) (io.ReadCloser, error)
}

func validateCalibration(raw []byte, modelLock, datasetLock string) error {
	return v2calibration.Validate(raw, modelLock, datasetLock)
}
func verifyCalibrationBytes(raw []byte, sha, modelLock, datasetLock string) error {
	return v2calibration.Verify(raw, sha, modelLock, datasetLock)
}
func VerifyCalibrationBytes(raw []byte, sha, modelLock, datasetLock string) error {
	return v2calibration.Verify(raw, sha, modelLock, datasetLock)
}
func CalibrationMinFaceEdge(raw []byte, sha, modelLock, datasetLock string) (int, error) {
	return v2calibration.CalibrationMinFaceEdge(raw, sha, modelLock, datasetLock)
}

func (s Service) verifyCalibration(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID,
	ref ArtifactRef, modelLock, datasetLock string) error {
	if s.Objects == nil || s.Bucket == "" {
		return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	var bucket string
	var object objectstore.Object
	err := tx.QueryRow(ctx, `SELECT bucket,object_key,object_version,sha256,bytes,media_type
	 FROM photo.artifacts WHERE tenant_id=$1 AND id=$2 AND sha256=$3
	 AND state='ready' AND verified_at IS NOT NULL AND object_version IS NOT NULL
	 AND (expires_at IS NULL OR expires_at>clock_timestamp())`, tenantID, ref.ArtifactID, ref.SHA256).
		Scan(&bucket, &object.Key, &object.VersionID, &object.SHA256, &object.Bytes, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) || bucket != s.Bucket || object.ContentType != "application/json" ||
		object.Bytes < 2 || object.Bytes > 1_048_576 {
		return domain.NewError("CALIBRATION_UNAVAILABLE", 503)
	}
	if err != nil {
		return err
	}
	body, err := s.Objects.Open(ctx, object)
	if err != nil {
		return err
	}
	defer body.Close()
	raw, err := io.ReadAll(io.LimitReader(body, 1_048_577))
	if err != nil {
		return err
	}
	if int64(len(raw)) != object.Bytes {
		return domain.NewError("CALIBRATION_UNAVAILABLE", 503)
	}
	return verifyCalibrationBytes(raw, ref.SHA256, modelLock, datasetLock)
}
