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

// SQLReviewedDefectObjects reads only the exact object versions exposed by
// the live, editor-authorized review/repair source. Every JSON object is
// reauthorized after physical IO; the caller still owns event association.
type SQLReviewedDefectObjects struct {
	DB      *pgxpool.Pool // photo_scheduler
	Objects LearningEvaluateObjectReader
	Bucket  string
}

func (s SQLReviewedDefectObjects) locate(ctx context.Context,
	tenantID, projectID, actorID, sessionID, reviewID uuid.UUID,
	role string, ref v2input.Ref) (objectstore.Object, error) {
	if s.DB == nil || s.Bucket == "" || tenantID == uuid.Nil ||
		projectID == uuid.Nil || actorID == uuid.Nil || sessionID == uuid.Nil ||
		reviewID == uuid.Nil || !validManifestRef(ref) {
		return objectstore.Object{}, ErrInvalidTrainingDraft
	}
	var object objectstore.Object
	var bucket string
	var sha string
	err := s.DB.QueryRow(ctx, `SELECT bucket,object_key,object_version,
 sha256,bytes,media_type FROM photo.v23_reviewed_defect_artifact(
 $1,$2,$3,$4,$5,$6,$7,$8)`, tenantID, projectID, actorID,
		sessionID, reviewID, role, ref.ArtifactID, ref.SHA256).Scan(
		&bucket, &object.Key, &object.VersionID, &sha,
		&object.Bytes, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return objectstore.Object{}, ErrTrainingDraftConflict
	}
	if err != nil {
		return objectstore.Object{}, err
	}
	if bucket != s.Bucket || sha != ref.SHA256 || object.Key == "" ||
		object.VersionID == "" || object.VersionID == "null" {
		return objectstore.Object{}, ErrInvalidTrainingDraft
	}
	object.SHA256 = sha
	return object, nil
}

// LoadJSON accepts only document roles; label and protection arrays need the
// dedicated bounded NPY path. The second lookup closes an ACL/revocation or
// object-version change between catalogue read and object delivery.
func (s SQLReviewedDefectObjects) LoadJSON(ctx context.Context,
	tenantID, projectID, actorID, sessionID, reviewID uuid.UUID,
	role string, ref v2input.Ref) ([]byte, error) {
	if s.Objects == nil || (role != "image" && role != "proposal" &&
		role != "selection" && role != "resolved" && role != "params") {
		return nil, ErrInvalidTrainingDraft
	}
	before, err := s.locate(ctx, tenantID, projectID, actorID, sessionID,
		reviewID, role, ref)
	if err != nil {
		return nil, err
	}
	body, err := ReadFrozenRootDocument(ctx, s.Objects, before, ref)
	if err != nil {
		return nil, err
	}
	after, err := s.locate(ctx, tenantID, projectID, actorID, sessionID,
		reviewID, role, ref)
	if err != nil {
		return nil, err
	}
	if before != after {
		return nil, ErrTrainingDraftConflict
	}
	return body, nil
}

// AuthorizeProtection returns only a current exact-version locator. A caller
// must use bounded mask-array IO and repeat this lookup before publication.
func (s SQLReviewedDefectObjects) AuthorizeProtection(ctx context.Context,
	tenantID, projectID, actorID, sessionID, reviewID uuid.UUID,
	ref v2input.Ref) (objectstore.Object, error) {
	object, err := s.locate(ctx, tenantID, projectID, actorID, sessionID,
		reviewID, "protection", ref)
	if err != nil {
		return objectstore.Object{}, err
	}
	if object.ContentType != "application/octet-stream" || object.Bytes < 2 ||
		object.Bytes > 801048576 {
		return objectstore.Object{}, ErrInvalidTrainingDraft
	}
	return object, nil
}
