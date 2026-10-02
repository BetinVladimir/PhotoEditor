package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type SessionAnalysisObjectReader interface {
	Open(context.Context, objectstore.Object) (io.ReadCloser, error)
}

type SQLSessionAnalysisImageSource struct {
	DB      *pgxpool.Pool
	Objects SessionAnalysisObjectReader
	Bucket  string
}

type sessionImageRow struct {
	Ordinal        int
	AssetVersionID uuid.UUID
	Ref            Ref
	Object         objectstore.Object
	Bucket         string
	CapturedAt     *string
	CameraHash     *string
}

func equalOptionalString(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func (s SQLSessionAnalysisImageSource) child(ctx context.Context, tenant, run uuid.UUID,
	parent Ref, child Ref, role string, maxBytes int64) error {
	var id uuid.UUID
	var sha, bucket string
	var object objectstore.Object
	err := s.DB.QueryRow(ctx, `SELECT artifact_id,sha256,bytes,bucket,object_key,
	 object_version,media_type FROM photo.v22_analysis_image_child($1,$2,$3,$4,$5,$6,$7)`,
		tenant, run, parent.ArtifactID, parent.SHA256, child.ArtifactID,
		child.SHA256, role).Scan(&id, &sha, &object.Bytes, &bucket,
		&object.Key, &object.VersionID, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: image child provenance", ErrInvalid)
	}
	if err != nil {
		return err
	}
	if id != child.ArtifactID || sha != child.SHA256 || bucket != s.Bucket ||
		object.VersionID == "" || object.VersionID == "null" ||
		object.ContentType != "application/octet-stream" ||
		object.Bytes < 1 || object.Bytes > maxBytes {
		return ErrInvalid
	}
	return nil
}

func readVerifiedSessionWorkingImage(ctx context.Context, objects SessionAnalysisObjectReader,
	object objectstore.Object, expected Ref) (review.WorkingImage, error) {
	if objects == nil || !validRef(expected) || object.SHA256 != expected.SHA256 ||
		object.ContentType != "application/json" || object.Bytes < 2 ||
		object.Bytes > 1048576 {
		return review.WorkingImage{}, ErrInvalid
	}
	reader, err := objects.Open(ctx, object)
	if err != nil {
		return review.WorkingImage{}, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, 1048577))
	closeErr := reader.Close()
	if readErr != nil {
		return review.WorkingImage{}, readErr
	}
	if closeErr != nil || ctx.Err() != nil {
		return review.WorkingImage{}, ErrInvalid
	}
	digest := sha256.Sum256(raw)
	if int64(len(raw)) != object.Bytes || len(raw) > 1048576 ||
		hex.EncodeToString(digest[:]) != expected.SHA256 {
		return review.WorkingImage{}, ErrInvalid
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil || image.DType != "float32" ||
		image.Color.ProfileID != "work_rec2020_gamma24_v1" {
		return review.WorkingImage{}, ErrInvalid
	}
	return image, nil
}

// ReadVerifiedWorkingImage validates the exact versioned source bytes used by
// both session admission and user draft binding. The caller must first prove
// that the artifact belongs to the authorized asset version.
func ReadVerifiedWorkingImage(ctx context.Context, objects SessionAnalysisObjectReader,
	object objectstore.Object, expected Ref) (review.WorkingImage, error) {
	return readVerifiedSessionWorkingImage(ctx, objects, object, expected)
}

func readSessionWorkingImage(ctx context.Context, objects SessionAnalysisObjectReader,
	object objectstore.Object, expected Ref, canvasHash string) (review.WorkingImage, error) {
	if !digestPattern.MatchString(canvasHash) {
		return review.WorkingImage{}, ErrInvalid
	}
	image, err := readVerifiedSessionWorkingImage(ctx, objects, object, expected)
	if err != nil {
		return review.WorkingImage{}, err
	}
	actualCanvasSHA, err := canonical.Hash(image.Canvas)
	if err != nil || actualCanvasSHA != canvasHash {
		return review.WorkingImage{}, ErrInvalid
	}
	return image, nil
}

// Load resolves only frozen snapshot WorkingImages, verifies their exact
// versioned bytes and requires each nested NPY to share the producer attempt.
// It returns one ordinal-aligned input row or fails the entire admission.
func (s SQLSessionAnalysisImageSource) Load(ctx context.Context,
	tenant, run uuid.UUID, items []SessionSnapshotItem) ([]SessionDescribeImageInputs, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || tenant == uuid.Nil ||
		run == uuid.Nil || len(items) < 1 || len(items) > 5000 {
		return nil, ErrInvalid
	}
	rows, err := s.DB.Query(ctx, `SELECT ordinal,asset_version_id,artifact_id,sha256,
	 bytes,bucket,object_key,object_version,media_type,captured_at,camera_hash
	 FROM photo.v22_analysis_image_sources($1,$2)`, tenant, run)
	if err != nil {
		return nil, err
	}
	var images []sessionImageRow
	for rows.Next() {
		var item sessionImageRow
		if err := rows.Scan(&item.Ordinal, &item.AssetVersionID,
			&item.Ref.ArtifactID, &item.Ref.SHA256, &item.Object.Bytes,
			&item.Bucket, &item.Object.Key, &item.Object.VersionID,
			&item.Object.ContentType, &item.CapturedAt, &item.CameraHash); err != nil {
			rows.Close()
			return nil, err
		}
		if len(images) >= len(items) || item.Ordinal != len(images) ||
			item.AssetVersionID != items[item.Ordinal].Binding.AssetVersionID ||
			item.Ref != items[item.Ordinal].WorkingImage ||
			item.Ref != items[item.Ordinal].Binding.Source ||
			!equalOptionalString(item.CapturedAt, items[item.Ordinal].CapturedAt) ||
			!equalOptionalString(item.CameraHash, items[item.Ordinal].CameraHash) ||
			item.Bucket != s.Bucket || !validRef(item.Ref) ||
			item.Object.VersionID == "" || item.Object.VersionID == "null" {
			rows.Close()
			return nil, ErrInvalid
		}
		item.Object.SHA256 = item.Ref.SHA256
		images = append(images, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(images) != len(items) {
		return nil, ErrInvalid
	}
	// The recipe must match the ref pinned in pending recovery and an immutable
	// document in the same asset-version draft stream. A later current-draft
	// change cannot alter this run's frozen recipe.
	recipeRows, err := s.DB.Query(ctx, `SELECT ordinal,asset_version_id,artifact_id,sha256,
	 bytes,bucket,object_key,object_version,media_type
	 FROM photo.v22_analysis_recipe_sources($1,$2)`, tenant, run)
	if err != nil {
		return nil, err
	}
	recipeCount := 0
	recipeObjects := make([]objectstore.Object, 0, len(items))
	for recipeRows.Next() {
		var ordinal int
		var version uuid.UUID
		var recipe Ref
		var object objectstore.Object
		var bucket string
		if err := recipeRows.Scan(&ordinal, &version, &recipe.ArtifactID,
			&recipe.SHA256, &object.Bytes, &bucket, &object.Key,
			&object.VersionID, &object.ContentType); err != nil {
			recipeRows.Close()
			return nil, err
		}
		if recipeCount >= len(items) || ordinal != recipeCount ||
			version != items[ordinal].Binding.AssetVersionID ||
			recipe != items[ordinal].Recipe || !validRef(recipe) ||
			bucket != s.Bucket {
			recipeRows.Close()
			return nil, ErrInvalid
		}
		object.SHA256 = recipe.SHA256
		recipeObjects = append(recipeObjects, object)
		recipeCount++
	}
	err = recipeRows.Err()
	recipeRows.Close()
	if err != nil {
		return nil, err
	}
	if recipeCount != len(items) {
		return nil, ErrInvalid
	}
	result := make([]SessionDescribeImageInputs, len(items))
	for i, item := range images {
		if err := readSessionRecipe(ctx, s.Objects, recipeObjects[i],
			items[i].Recipe, items[i].Binding); err != nil {
			return nil, err
		}
		image, err := readSessionWorkingImage(ctx, s.Objects, item.Object,
			item.Ref, items[i].Binding.CanvasHash)
		if err != nil {
			return nil, err
		}
		pixels := Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}
		if err := s.child(ctx, tenant, run, item.Ref, pixels,
			"working_rgb", 2401048576); err != nil {
			return nil, err
		}
		result[i] = SessionDescribeImageInputs{Ordinal: i, Pixels: pixels}
		if image.Alpha != nil {
			alpha := Ref{ArtifactID: image.Alpha.ID, SHA256: image.Alpha.SHA256}
			if err := s.child(ctx, tenant, run, item.Ref, alpha,
				"alpha", 401048576); err != nil {
				return nil, err
			}
			result[i].Alpha = &alpha
		}
	}
	return result, nil
}
