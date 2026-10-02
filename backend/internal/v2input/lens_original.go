package v2input

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

// SQLLensOriginalSource resolves provenance for compiler inputs. Load verifies
// immutable source bytes before matching their original-content and canvas hashes.
type SQLLensOriginalSource struct {
	DB      AnalysisStageQuerier
	Bucket  string
	Objects SessionAnalysisObjectReader
}
type LensOriginalSource struct {
	Ref    Ref
	Object objectstore.Object
}

func (s SQLLensOriginalSource) resolve(ctx context.Context, tenant, project, actor, session uuid.UUID,
	binding SessionVersionBinding, image review.WorkingImage) (LensOriginalSource, error) {
	var result LensOriginalSource
	if s.DB == nil || s.Bucket == "" || tenant == uuid.Nil || project == uuid.Nil || actor == uuid.Nil || session == uuid.Nil ||
		binding.AssetVersionID == uuid.Nil || !validRef(binding.Source) {
		return result, ErrInvalid
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil || canvas != binding.CanvasHash {
		return result, ErrInvalid
	}
	var bucket string
	err = s.DB.QueryRow(ctx, `SELECT artifact_id,sha256,bytes,bucket,object_key,object_version,media_type
 FROM photo.v25_lens_original_source($1,$2,$3,$4,$5,$6,$7)`, tenant, project, binding.AssetVersionID,
		binding.Source.ArtifactID, binding.Source.SHA256, actor, session).Scan(&result.Ref.ArtifactID, &result.Ref.SHA256,
		&result.Object.Bytes, &bucket, &result.Object.Key, &result.Object.VersionID, &result.Object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return LensOriginalSource{}, ErrExpired
	}
	if err != nil {
		return LensOriginalSource{}, err
	}
	if !validRef(result.Ref) || result.Ref.SHA256 != image.Canvas.SourceSHA256 || bucket != s.Bucket ||
		result.Object.Bytes < 1 || result.Object.Key == "" || result.Object.VersionID == "" || result.Object.VersionID == "null" || ctx.Err() != nil {
		return LensOriginalSource{}, ErrInvalid
	}
	result.Object.SHA256 = result.Ref.SHA256
	return result, nil
}

// Load takes a source object descriptor already resolved in the caller's authorized
// artifact scope. JSON metadata cannot bypass SHA, length, schema or canvas checks.
func (s SQLLensOriginalSource) Load(ctx context.Context, tenant, project, actor, session uuid.UUID,
	binding SessionVersionBinding, source objectstore.Object) (LensOriginalSource, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || source.Key == "" || source.VersionID == "" || source.VersionID == "null" ||
		tenant == uuid.Nil || project == uuid.Nil || actor == uuid.Nil || session == uuid.Nil || binding.AssetVersionID == uuid.Nil {
		return LensOriginalSource{}, ErrInvalid
	}
	image, err := readSessionWorkingImage(ctx, s.Objects, source, binding.Source, binding.CanvasHash)
	if err != nil {
		return LensOriginalSource{}, err
	}
	return s.resolve(ctx, tenant, project, actor, session, binding, image)
}

// LensOriginalBinding matches the worker's optical.metadata_source contract.
// Container type comes from catalog media metadata, never an editor filename.
type LensOriginalBinding struct {
	Binding  SessionVersionBinding `json:"binding"`
	Original Ref                   `json:"original"`
	Suffix   string                `json:"suffix"`
}

func CompileLensOriginalBinding(binding SessionVersionBinding, original LensOriginalSource) (LensOriginalBinding, error) {
	if binding.AssetVersionID == uuid.Nil || !validRef(binding.Source) || !digestPattern.MatchString(binding.CanvasHash) ||
		!validRef(original.Ref) || original.Object.SHA256 != original.Ref.SHA256 || original.Object.Bytes < 1 ||
		original.Object.Key == "" || original.Object.VersionID == "" || original.Object.VersionID == "null" {
		return LensOriginalBinding{}, ErrInvalid
	}
	suffixes := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/tiff": ".tiff", "image/x-adobe-dng": ".dng",
		"image/x-nikon-nef": ".nef", "image/x-canon-cr2": ".cr2", "image/x-canon-cr3": ".cr3", "image/x-sony-arw": ".arw",
		"image/x-fuji-raf": ".raf", "image/x-panasonic-rw2": ".rw2", "image/x-olympus-orf": ".orf", "image/x-pentax-pef": ".pef"}
	suffix, ok := suffixes[original.Object.ContentType]
	if !ok {
		return LensOriginalBinding{}, ErrInvalid
	}
	return LensOriginalBinding{binding, original.Ref, suffix}, nil
}
