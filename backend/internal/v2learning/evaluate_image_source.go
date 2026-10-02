package v2learning

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type FrozenEvaluationImage struct {
	Pixels     v2input.Ref
	Alpha      *v2input.Ref
	PixelCount int64
}

type SQLLearningEvaluateImageSource struct {
	DB     *pgxpool.Pool
	Bucket string
}

// Resolve reads metadata only after the WorkingImage root bytes have been
// verified. SQL proves each NPY comes from that root's producer fence.
func (s SQLLearningEvaluateImageSource) Resolve(ctx context.Context,
	lease LearningEvaluateLease, parent v2input.Ref, raw []byte,
	binding v2input.SessionVersionBinding) (FrozenEvaluationImage, error) {
	if s.DB == nil || s.Bucket == "" || !validLearningEvaluateLease(lease) ||
		!validManifestRef(parent) || binding.Source != parent {
		return FrozenEvaluationImage{}, ErrInvalidTrainingDraft
	}
	image, err := ParseFrozenEvaluationImage(raw, binding)
	if err != nil {
		return FrozenEvaluationImage{}, err
	}
	pixels := v2input.Ref{ArtifactID: image.Pixels.ID,
		SHA256: image.Pixels.SHA256}
	if err := s.child(ctx, lease, parent, pixels, "working_rgb"); err != nil {
		return FrozenEvaluationImage{}, err
	}
	var alpha *v2input.Ref
	if image.Alpha != nil {
		ref := v2input.Ref{ArtifactID: image.Alpha.ID,
			SHA256: image.Alpha.SHA256}
		if err := s.child(ctx, lease, parent, ref, "alpha"); err != nil {
			return FrozenEvaluationImage{}, err
		}
		alpha = &ref
	}
	return FrozenEvaluationImage{Pixels: pixels, Alpha: alpha,
		PixelCount: image.Canvas.Width * image.Canvas.Height}, nil
}

func ParseFrozenEvaluationImage(raw []byte,
	binding v2input.SessionVersionBinding) (review.WorkingImage, error) {
	image, err := review.ParseWorkingImage(raw)
	if err != nil || image.DType != "float32" ||
		image.Color.ProfileID != "work_rec2020_gamma24_v1" {
		return review.WorkingImage{}, ErrInvalidTrainingDraft
	}
	canvasSHA, err := canonical.Hash(image.Canvas)
	if err != nil || canvasSHA != binding.CanvasHash {
		return review.WorkingImage{}, ErrInvalidTrainingDraft
	}
	return image, nil
}

func (s SQLLearningEvaluateImageSource) child(ctx context.Context,
	lease LearningEvaluateLease, parent, child v2input.Ref,
	role string) error {
	if !validManifestRef(child) || (role != "working_rgb" && role != "alpha") {
		return ErrInvalidTrainingDraft
	}
	var found v2input.Ref
	var object objectstore.Object
	var bucket string
	err := s.DB.QueryRow(ctx, `SELECT artifact_id,sha256,bytes,bucket,
	 object_key,object_version,media_type
	 FROM photo.v23_evaluate_image_child($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		lease.TenantID, lease.RunID, lease.ExpansionID, lease.Token,
		parent.ArtifactID, parent.SHA256, child.ArtifactID,
		child.SHA256, role).Scan(&found.ArtifactID, &found.SHA256,
		&object.Bytes, &bucket, &object.Key, &object.VersionID,
		&object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTrainingDraftConflict
	}
	if err != nil {
		return err
	}
	if found != child || bucket != s.Bucket || object.Bytes < 32 ||
		object.Key == "" || object.VersionID == "" ||
		object.VersionID == "null" ||
		object.ContentType != "application/octet-stream" {
		return ErrInvalidTrainingDraft
	}
	return nil
}
