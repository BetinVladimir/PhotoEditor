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

// SQLReviewedDefectEvidenceReader reads one registered, event-owned object.
// The locator rechecks the live review decision and editor session on both
// sides of exact-version object IO. This reader does not enable Evaluate.
type SQLReviewedDefectEvidenceReader struct {
	DB      *pgxpool.Pool // photo_scheduler
	Objects LearningEvaluateObjectReader
	Bucket  string
}

func (s SQLReviewedDefectEvidenceReader) locate(ctx context.Context,
	draft TrainingDraftPrepared, expected DefectHoldoutExpected,
	ref v2input.Ref) (objectstore.Object, error) {
	if s.DB == nil || s.Bucket == "" || draft.TenantID == uuid.Nil ||
		draft.RunID == uuid.Nil || draft.ActorID == uuid.Nil ||
		draft.AuthSessionID == uuid.Nil || expected.TenantID != draft.TenantID ||
		expected.EventID == uuid.Nil || expected.ReviewID == uuid.Nil ||
		!validSHA(expected.DecisionHash) || !validManifestRef(ref) {
		return objectstore.Object{}, ErrInvalidTrainingDraft
	}
	stable, err := ReviewedDefectHoldoutRef(draft.RunID,
		expected.EventID, ref.SHA256)
	if err != nil || stable != ref {
		return objectstore.Object{}, ErrInvalidTrainingDraft
	}
	var object objectstore.Object
	var bucket, sha string
	err = s.DB.QueryRow(ctx, `SELECT bucket,object_key,object_version,
 sha256,bytes,media_type FROM photo.v23_reviewed_defect_document_locator(
 $1,$2,$3,$4,$5,$6,$7,$8,$9)`, draft.TenantID, draft.RunID,
		expected.EventID, expected.ReviewID, draft.ActorID,
		draft.AuthSessionID, expected.DecisionHash, ref.ArtifactID,
		ref.SHA256).Scan(&bucket, &object.Key, &object.VersionID,
		&sha, &object.Bytes, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return objectstore.Object{}, ErrTrainingDraftConflict
	}
	if err != nil {
		return objectstore.Object{}, err
	}
	if bucket != s.Bucket || sha != ref.SHA256 || object.Key == "" ||
		object.VersionID == "" || object.VersionID == "null" ||
		object.ContentType != "application/json" ||
		object.Bytes < 2 || object.Bytes > 1<<20 {
		return objectstore.Object{}, ErrInvalidTrainingDraft
	}
	object.SHA256 = sha
	return object, nil
}

func (s SQLReviewedDefectEvidenceReader) Load(ctx context.Context,
	draft TrainingDraftPrepared, expected DefectHoldoutExpected,
	ref v2input.Ref) (FrozenDefectHoldoutEvidence, error) {
	if s.Objects == nil {
		return FrozenDefectHoldoutEvidence{}, ErrInvalidTrainingDraft
	}
	before, err := s.locate(ctx, draft, expected, ref)
	if err != nil {
		return FrozenDefectHoldoutEvidence{}, err
	}
	body, err := ReadFrozenRootDocument(ctx, s.Objects, before, ref)
	if err != nil {
		return FrozenDefectHoldoutEvidence{}, err
	}
	after, err := s.locate(ctx, draft, expected, ref)
	if err != nil {
		return FrozenDefectHoldoutEvidence{}, err
	}
	if before != after {
		return FrozenDefectHoldoutEvidence{}, ErrTrainingDraftConflict
	}
	return DecodeFrozenDefectHoldoutEvidence(body, ref, expected)
}
