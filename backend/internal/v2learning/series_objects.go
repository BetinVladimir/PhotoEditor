package v2learning

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// SQLSeriesObjects reads only current event-selected, version-pinned Apply
// documents. It does not grant Fit/Evaluate access to those documents.
type SQLSeriesObjects struct {
	DB      *pgxpool.Pool // photo_scheduler
	Objects LearningEvaluateObjectReader
	Bucket  string
}

func validSeriesObjectRole(role string) bool {
	return role == "source_image" || role == "stage_image" ||
		role == "params" || role == "series"
}

func (s SQLSeriesObjects) locate(ctx context.Context,
	start TrainingStart, eventID uuid.UUID, role string,
	ref v2input.Ref) (objectstore.Object, error) {
	if s.DB == nil || s.Bucket == "" || start.TenantID == uuid.Nil ||
		start.ProjectID == uuid.Nil || start.ActorID == uuid.Nil ||
		start.AuthSessionID == uuid.Nil || eventID == uuid.Nil ||
		!validManifestRef(ref) || !validSeriesObjectRole(role) {
		return objectstore.Object{}, ErrInvalidTrainingDraft
	}
	var object objectstore.Object
	var bucket, sha string
	err := s.DB.QueryRow(ctx, `SELECT bucket,object_key,object_version,
 sha256,bytes,media_type FROM photo.v23_series_artifact(
 $1,$2,$3,$4,$5,$6,$7,$8)`, start.TenantID, start.ProjectID,
		start.ActorID, start.AuthSessionID, eventID, role,
		ref.ArtifactID, ref.SHA256).Scan(&bucket, &object.Key,
		&object.VersionID, &sha, &object.Bytes, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return objectstore.Object{}, ErrTrainingDraftConflict
	}
	if err != nil {
		return objectstore.Object{}, err
	}
	object.SHA256 = sha
	if bucket != s.Bucket || sha != ref.SHA256 || object.Key == "" ||
		object.VersionID == "" || object.VersionID == "null" ||
		object.ContentType != "application/json" ||
		object.Bytes < 2 || object.Bytes > 1<<20 {
		return objectstore.Object{}, ErrInvalidTrainingDraft
	}
	return object, nil
}

// LoadJSON repeats source authority after IO so a revocation or source
// replacement cannot be hidden behind an already obtained object locator.
func (s SQLSeriesObjects) LoadJSON(ctx context.Context,
	start TrainingStart, eventID uuid.UUID, role string,
	ref v2input.Ref) ([]byte, error) {
	if s.Objects == nil {
		return nil, ErrInvalidTrainingDraft
	}
	before, err := s.locate(ctx, start, eventID, role, ref)
	if err != nil {
		return nil, err
	}
	body, err := ReadFrozenRootDocument(ctx, s.Objects, before, ref)
	if err != nil {
		return nil, err
	}
	after, err := s.locate(ctx, start, eventID, role, ref)
	if err != nil {
		return nil, err
	}
	if before != after {
		return nil, ErrTrainingDraftConflict
	}
	return body, nil
}

type seriesStatLocator struct {
	ItemID uuid.UUID
	Image  v2input.Ref
	Object objectstore.Object
}

func (s SQLSeriesObjects) locateStat(ctx context.Context,
	start TrainingStart, eventID uuid.UUID,
	ref v2input.Ref) (seriesStatLocator, error) {
	if s.DB == nil || s.Bucket == "" || start.TenantID == uuid.Nil ||
		start.ProjectID == uuid.Nil || start.ActorID == uuid.Nil ||
		start.AuthSessionID == uuid.Nil || eventID == uuid.Nil ||
		!validManifestRef(ref) {
		return seriesStatLocator{}, ErrInvalidTrainingDraft
	}
	var result seriesStatLocator
	var bucket, sha string
	err := s.DB.QueryRow(ctx, `SELECT item_id,image_id,image_sha,bucket,
 object_key,object_version,sha256,bytes,media_type
 FROM photo.v23_series_stat_artifact($1,$2,$3,$4,$5,$6,$7)`,
		start.TenantID, start.ProjectID, start.ActorID,
		start.AuthSessionID, eventID, ref.ArtifactID, ref.SHA256).Scan(
		&result.ItemID, &result.Image.ArtifactID, &result.Image.SHA256,
		&bucket, &result.Object.Key, &result.Object.VersionID,
		&sha, &result.Object.Bytes, &result.Object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return seriesStatLocator{}, ErrTrainingDraftConflict
	}
	if err != nil {
		return seriesStatLocator{}, err
	}
	result.Object.SHA256 = sha
	if result.ItemID == uuid.Nil || !validManifestRef(result.Image) ||
		bucket != s.Bucket || sha != ref.SHA256 ||
		result.Object.Key == "" || result.Object.VersionID == "" ||
		result.Object.VersionID == "null" ||
		result.Object.ContentType != "application/json" ||
		result.Object.Bytes < 2 || result.Object.Bytes > 1<<20 {
		return seriesStatLocator{}, ErrInvalidTrainingDraft
	}
	return result, nil
}

// LoadStatJSON locates an input of the exact Apply balance and repeats all
// source checks after reading the pinned object version.
func (s SQLSeriesObjects) LoadStatJSON(ctx context.Context,
	start TrainingStart, eventID uuid.UUID,
	ref v2input.Ref) ([]byte, uuid.UUID, v2input.Ref, error) {
	if s.Objects == nil {
		return nil, uuid.Nil, v2input.Ref{}, ErrInvalidTrainingDraft
	}
	before, err := s.locateStat(ctx, start, eventID, ref)
	if err != nil {
		return nil, uuid.Nil, v2input.Ref{}, err
	}
	body, err := ReadFrozenRootDocument(ctx, s.Objects, before.Object, ref)
	if err != nil {
		return nil, uuid.Nil, v2input.Ref{}, err
	}
	after, err := s.locateStat(ctx, start, eventID, ref)
	if err != nil {
		return nil, uuid.Nil, v2input.Ref{}, err
	}
	if before != after {
		return nil, uuid.Nil, v2input.Ref{}, ErrTrainingDraftConflict
	}
	return body, before.ItemID, before.Image, nil
}
