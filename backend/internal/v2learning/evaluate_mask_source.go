package v2learning

import (
	"bytes"
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type SQLLearningEvaluateMaskSource struct {
	DB     *pgxpool.Pool
	Bucket string
}

// ParseFrozenProtectedMask requires the same canvas and transform used to
// render the holdout image; otherwise the pixel guard cannot be measured.
func ParseFrozenProtectedMask(raw []byte,
	image review.WorkingImage) (v2input.Ref, error) {
	mask, err := review.ParseMaskRef(raw)
	if err != nil || !mask.Protected || mask.Canvas != image.Canvas ||
		!bytes.Equal(mask.TransformChain, image.TransformChain) {
		return v2input.Ref{}, ErrInvalidTrainingDraft
	}
	return v2input.Ref{ArtifactID: mask.Artifact.ID,
		SHA256: mask.Artifact.SHA256}, nil
}

// ParseFrozenEffectiveMask binds the editable person region to the same
// source canvas and transform as the holdout image.
func ParseFrozenEffectiveMask(raw []byte,
	image review.WorkingImage) (v2input.Ref, error) {
	mask, err := review.ParseMaskRef(raw)
	if err != nil || mask.Protected || mask.Canvas != image.Canvas ||
		!bytes.Equal(mask.TransformChain, image.TransformChain) {
		return v2input.Ref{}, ErrInvalidTrainingDraft
	}
	return v2input.Ref{ArtifactID: mask.Artifact.ID,
		SHA256: mask.Artifact.SHA256}, nil
}

func (s SQLLearningEvaluateMaskSource) ResolveEffective(ctx context.Context,
	lease LearningEvaluateLease, parent v2input.Ref, raw []byte,
	image review.WorkingImage) (v2input.Ref, error) {
	if s.DB == nil || s.Bucket == "" || !validLearningEvaluateLease(lease) ||
		!validManifestRef(parent) {
		return v2input.Ref{}, ErrInvalidTrainingDraft
	}
	child, err := ParseFrozenEffectiveMask(raw, image)
	if err != nil {
		return v2input.Ref{}, err
	}
	var found v2input.Ref
	var object objectstore.Object
	var bucket, role string
	err = s.DB.QueryRow(ctx, `SELECT artifact_id,sha256,bytes,bucket,
	 object_key,object_version,media_type,output_role
	 FROM photo.v23_evaluate_effective_mask_child($1,$2,$3,$4,$5,$6,$7,$8)`,
		lease.TenantID, lease.RunID, lease.ExpansionID, lease.Token,
		parent.ArtifactID, parent.SHA256, child.ArtifactID,
		child.SHA256).Scan(&found.ArtifactID, &found.SHA256,
		&object.Bytes, &bucket, &object.Key, &object.VersionID,
		&object.ContentType, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return v2input.Ref{}, ErrTrainingDraftConflict
	}
	if err != nil {
		return v2input.Ref{}, err
	}
	if found != child || bucket != s.Bucket || object.Bytes < 32 ||
		object.Key == "" || object.VersionID == "" ||
		object.VersionID == "null" ||
		object.ContentType != "application/octet-stream" ||
		(role != "person_mask" && role != "mask_target" &&
			role != "transformed_mask") {
		return v2input.Ref{}, ErrInvalidTrainingDraft
	}
	return child, nil
}

func (s SQLLearningEvaluateMaskSource) Resolve(ctx context.Context,
	lease LearningEvaluateLease, parent v2input.Ref, raw []byte,
	image review.WorkingImage) (v2input.Ref, error) {
	if s.DB == nil || s.Bucket == "" || !validLearningEvaluateLease(lease) ||
		!validManifestRef(parent) {
		return v2input.Ref{}, ErrInvalidTrainingDraft
	}
	child, err := ParseFrozenProtectedMask(raw, image)
	if err != nil {
		return v2input.Ref{}, err
	}
	var found v2input.Ref
	var object objectstore.Object
	var bucket, role string
	err = s.DB.QueryRow(ctx, `SELECT artifact_id,sha256,bytes,bucket,
	 object_key,object_version,media_type,output_role
	 FROM photo.v23_evaluate_protected_mask_child($1,$2,$3,$4,$5,$6,$7,$8)`,
		lease.TenantID, lease.RunID, lease.ExpansionID, lease.Token,
		parent.ArtifactID, parent.SHA256, child.ArtifactID,
		child.SHA256).Scan(&found.ArtifactID, &found.SHA256,
		&object.Bytes, &bucket, &object.Key, &object.VersionID,
		&object.ContentType, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return v2input.Ref{}, ErrTrainingDraftConflict
	}
	if err != nil {
		return v2input.Ref{}, err
	}
	if found != child || bucket != s.Bucket || object.Bytes < 32 ||
		object.Key == "" || object.VersionID == "" ||
		object.VersionID == "null" ||
		object.ContentType != "application/octet-stream" ||
		(role != "mask_protected" && role != "defect_protection" &&
			role != "transformed_mask") {
		return v2input.Ref{}, ErrInvalidTrainingDraft
	}
	return child, nil
}
