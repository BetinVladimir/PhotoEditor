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

// SQLReviewedDustObjects resolves each JSON document through the current
// event-selected review on both sides of exact-version object IO.
type SQLReviewedDustObjects struct {
	DB      *pgxpool.Pool // photo_scheduler
	Objects LearningEvaluateObjectReader
	Bucket  string
}

func (s SQLReviewedDustObjects) locate(ctx context.Context,
	start TrainingStart, eventID uuid.UUID, role string,
	ref v2input.Ref) (objectstore.Object, error) {
	if s.DB == nil || s.Bucket == "" || start.TenantID == uuid.Nil ||
		start.ProjectID == uuid.Nil || start.ActorID == uuid.Nil ||
		start.AuthSessionID == uuid.Nil || eventID == uuid.Nil ||
		!validManifestRef(ref) ||
		(role != "image" && role != "stage_image" &&
			role != "proposal" && role != "params") {
		return objectstore.Object{}, ErrInvalidTrainingDraft
	}
	var object objectstore.Object
	var bucket, sha string
	err := s.DB.QueryRow(ctx, `SELECT bucket,object_key,object_version,
 sha256,bytes,media_type FROM photo.v23_reviewed_dust_artifact(
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
	if bucket != s.Bucket || sha != ref.SHA256 ||
		object.Key == "" || object.VersionID == "" ||
		object.VersionID == "null" || object.ContentType != "application/json" ||
		object.Bytes < 2 || object.Bytes > 1<<20 {
		return objectstore.Object{}, ErrInvalidTrainingDraft
	}
	object.SHA256 = sha
	return object, nil
}

func (s SQLReviewedDustObjects) LoadJSON(ctx context.Context,
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
