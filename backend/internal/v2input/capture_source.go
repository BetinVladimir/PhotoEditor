package v2input

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type CaptureSourceImage struct {
	Binding     SessionVersionBinding
	Image       review.WorkingImage
	OriginalSHA string
}
type SQLCaptureSource struct {
	DB      *pgxpool.Pool
	Objects SessionAnalysisObjectReader
	Bucket  string
}

// Load verifies exact versioned bytes after the narrow SQL function proves the
// receipt binding. It never hashes the JSON document as original photo content.
func (s SQLCaptureSource) Load(ctx context.Context, l CaptureAssessmentLease) (CaptureSourceImage, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || !validCaptureLease(l) {
		return CaptureSourceImage{}, ErrInvalid
	}
	var version uuid.UUID
	var ref Ref
	var obj objectstore.Object
	var bucket, original string
	err := s.DB.QueryRow(ctx, `SELECT asset_version_id,artifact_id,sha256,bytes,bucket,
 object_key,object_version,media_type,original_sha FROM photo.capture_assessment_source($1,$2,$3)`,
		l.TenantID, l.JobID, l.Token).Scan(&version, &ref.ArtifactID, &ref.SHA256, &obj.Bytes, &bucket, &obj.Key, &obj.VersionID, &obj.ContentType, &original)
	if errors.Is(err, pgx.ErrNoRows) {
		return CaptureSourceImage{}, ErrExpired
	}
	if err != nil {
		return CaptureSourceImage{}, err
	}
	if version != l.AssetVersionID || bucket != s.Bucket || obj.VersionID == "" || obj.VersionID == "null" || !digestPattern.MatchString(original) {
		return CaptureSourceImage{}, ErrInvalid
	}
	obj.SHA256 = ref.SHA256
	image, err := ReadVerifiedWorkingImage(ctx, s.Objects, obj, ref)
	if err != nil {
		return CaptureSourceImage{}, err
	}
	if image.Canvas.SourceSHA256 != original {
		return CaptureSourceImage{}, ErrInvalid
	}
	canvasSHA, err := canonical.Hash(image.Canvas)
	if err != nil {
		return CaptureSourceImage{}, err
	}
	return CaptureSourceImage{SessionVersionBinding{version, ref, canvasSHA}, image, original}, nil
}

func (s SQLCaptureSource) LoadInputs(ctx context.Context, l CaptureAssessmentLease) (CaptureSourceImage, CaptureImageInputs, error) {
	source, err := s.Load(ctx, l)
	if err != nil {
		return CaptureSourceImage{}, CaptureImageInputs{}, err
	}
	result := CaptureImageInputs{Pixels: Ref{ArtifactID: source.Image.Pixels.ID, SHA256: source.Image.Pixels.SHA256}}
	if source.Image.Alpha != nil {
		result.Alpha = &Ref{ArtifactID: source.Image.Alpha.ID, SHA256: source.Image.Alpha.SHA256}
	}
	children := []struct {
		ref  Ref
		role string
		max  int64
	}{{result.Pixels, "working_rgb", source.Image.Canvas.Width*source.Image.Canvas.Height*12 + 8192}}
	if result.Alpha != nil {
		children = append(children, struct {
			ref  Ref
			role string
			max  int64
		}{*result.Alpha, "alpha", source.Image.Canvas.Width*source.Image.Canvas.Height*2 + 8192})
	}
	for _, child := range children {
		var ref Ref
		var obj objectstore.Object
		var bucket string
		err = s.DB.QueryRow(ctx, `SELECT artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.capture_source_child($1,$2,$3,$4,$5,$6)`,
			l.TenantID, l.JobID, l.Token, child.ref.ArtifactID, child.ref.SHA256, child.role).Scan(&ref.ArtifactID, &ref.SHA256, &obj.Bytes, &bucket, &obj.Key, &obj.VersionID, &obj.ContentType)
		if err != nil {
			return CaptureSourceImage{}, CaptureImageInputs{}, err
		}
		if ref != child.ref || bucket != s.Bucket || obj.Bytes < 1 || obj.Bytes > child.max || obj.ContentType != "application/octet-stream" || obj.VersionID == "" || obj.VersionID == "null" {
			return CaptureSourceImage{}, CaptureImageInputs{}, ErrInvalid
		}
	}
	return source, result, nil
}
