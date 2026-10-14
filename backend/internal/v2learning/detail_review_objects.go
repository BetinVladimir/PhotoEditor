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

// SQLPortraitDetailObjects reads only registered, exact-version JSON inputs
// of the event-linked producer. Protection and mask objects have a separate
// provenance path and are deliberately unavailable through this reader.
type SQLPortraitDetailObjects struct {
	DB      *pgxpool.Pool // photo_scheduler
	Objects LearningEvaluateObjectReader
	Bucket  string
}

func validPortraitDetailObjectRole(role string) bool {
	return role == "image" || role == "params" || role == "resolved"
}

func validPortraitDetailObject(object objectstore.Object, bucket, sha,
	expectedBucket string) bool {
	return bucket == expectedBucket && sha == object.SHA256 &&
		object.Key != "" && object.VersionID != "" &&
		object.VersionID != "null" &&
		object.ContentType == "application/json" &&
		object.Bytes >= 2 && object.Bytes <= 1<<20
}

func (s SQLPortraitDetailObjects) locate(ctx context.Context,
	start TrainingStart, eventID uuid.UUID, role string,
	ref v2input.Ref) (objectstore.Object, error) {
	if s.DB == nil || s.Bucket == "" || start.TenantID == uuid.Nil ||
		start.ProjectID == uuid.Nil || start.ActorID == uuid.Nil ||
		start.AuthSessionID == uuid.Nil || eventID == uuid.Nil ||
		!validManifestRef(ref) || !validPortraitDetailObjectRole(role) {
		return objectstore.Object{}, ErrInvalidTrainingDraft
	}
	var object objectstore.Object
	var bucket, sha string
	err := s.DB.QueryRow(ctx, `SELECT bucket,object_key,object_version,
 sha256,bytes,media_type FROM photo.v23_portrait_detail_artifact(
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
	if !validPortraitDetailObject(object, bucket, sha, s.Bucket) ||
		sha != ref.SHA256 {
		return objectstore.Object{}, ErrInvalidTrainingDraft
	}
	return object, nil
}

func (s SQLPortraitDetailObjects) LoadJSON(ctx context.Context,
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
