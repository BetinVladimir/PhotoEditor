package v2input

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/objectstore"
)

// SQLDeliveryMetadataOriginal resolves the immutable original through Delivery's
// authenticated SQL authority. The caller supplies an already authorized source
// descriptor; its exact bytes and canvas are checked before any original lookup.
type SQLDeliveryMetadataOriginal struct {
	DB      AnalysisStageQuerier
	Bucket  string
	Objects SessionAnalysisObjectReader
}

func (s SQLDeliveryMetadataOriginal) Load(ctx context.Context, tenant, project, actor, session uuid.UUID,
	binding SessionVersionBinding, source objectstore.Object) (LensOriginalSource, error) {
	if err := ctx.Err(); err != nil {
		return LensOriginalSource{}, err
	}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || tenant == uuid.Nil || project == uuid.Nil || actor == uuid.Nil || session == uuid.Nil ||
		binding.AssetVersionID == uuid.Nil || !validRef(binding.Source) || source.Key == "" || !objectstore.ValidInterchangeVersionID(source.VersionID) {
		return LensOriginalSource{}, ErrInvalid
	}
	image, err := readSessionWorkingImage(ctx, s.Objects, source, binding.Source, binding.CanvasHash)
	if err != nil {
		return LensOriginalSource{}, err
	}
	if err = ctx.Err(); err != nil {
		return LensOriginalSource{}, err
	}
	var result LensOriginalSource
	var bucket string
	err = s.DB.QueryRow(ctx, `SELECT artifact_id,sha256,bytes,bucket,object_key,object_version,media_type
 FROM photo.v30_delivery_metadata_original($1,$2,$3,$4,$5,$6,$7)`, tenant, project, binding.AssetVersionID,
		binding.Source.ArtifactID, binding.Source.SHA256, actor, session).Scan(&result.Ref.ArtifactID, &result.Ref.SHA256,
		&result.Object.Bytes, &bucket, &result.Object.Key, &result.Object.VersionID, &result.Object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return LensOriginalSource{}, ErrExpired
	}
	if err != nil {
		return LensOriginalSource{}, err
	}
	if err = ctx.Err(); err != nil {
		return LensOriginalSource{}, err
	}
	if !validRef(result.Ref) || result.Ref.SHA256 != image.Canvas.SourceSHA256 || bucket != s.Bucket ||
		result.Object.Bytes < 1 || result.Object.Bytes > 2*1024*1024*1024 || result.Object.Key == "" ||
		!objectstore.ValidInterchangeVersionID(result.Object.VersionID) {
		return LensOriginalSource{}, ErrInvalid
	}
	// Only formats with an installed metadata preparer and exact physical type check.
	switch result.Object.ContentType {
	case "image/jpeg", "image/png", "image/tiff",
		"image/x-adobe-dng", "image/x-canon-cr2", "image/x-nikon-nef", "image/x-sony-arw",
		"image/x-fuji-raf", "image/x-olympus-orf", "image/x-panasonic-rw2":
	default:
		return LensOriginalSource{}, ErrInvalid
	}
	result.Object.SHA256 = result.Ref.SHA256
	return result, nil
}
