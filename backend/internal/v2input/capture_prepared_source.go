package v2input

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
)

type CapturePreparedDocument struct {
	SchemaVersion int                   `json:"schema_version"`
	Binding       SessionVersionBinding `json:"binding"`
	Proxy         Ref                   `json:"proxy"`
	Faces         Ref                   `json:"faces"`
}
type SQLCapturePreparedSource struct {
	DB      AnalysisStageQuerier
	Objects SessionAnalysisObjectReader
	Bucket  string
}

func (s SQLCapturePreparedSource) Load(ctx context.Context, tenant, run, workflow uuid.UUID) (CapturePreparedDocument, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || tenant == uuid.Nil || run == uuid.Nil || workflow == uuid.Nil {
		return CapturePreparedDocument{}, ErrInvalid
	}
	var task uuid.UUID
	var ref Ref
	var object objectstore.Object
	var bucket string
	err := s.DB.QueryRow(ctx, `SELECT task_id,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type
 FROM photo.capture_prepared_source($1,$2)`, tenant, run).Scan(&task, &ref.ArtifactID, &ref.SHA256, &object.Bytes, &bucket, &object.Key, &object.VersionID, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return CapturePreparedDocument{}, ErrExpired
	}
	if err != nil {
		return CapturePreparedDocument{}, err
	}
	expected, err := TaskID(workflow, "capture.prepare", 0)
	if err != nil || task != expected || !validRef(ref) || bucket != s.Bucket || object.Bytes < 2 || object.Bytes > 8<<20 ||
		object.VersionID == "" || object.VersionID == "null" || object.ContentType != "application/json" {
		return CapturePreparedDocument{}, ErrInvalid
	}
	object.SHA256 = ref.SHA256
	reader, err := s.Objects.Open(ctx, object)
	if err != nil {
		return CapturePreparedDocument{}, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, (8<<20)+1))
	closeErr := reader.Close()
	if readErr != nil {
		return CapturePreparedDocument{}, readErr
	}
	digest := sha256.Sum256(raw)
	if closeErr != nil || ctx.Err() != nil || int64(len(raw)) != object.Bytes || hex.EncodeToString(digest[:]) != ref.SHA256 {
		return CapturePreparedDocument{}, ErrInvalid
	}
	var result CapturePreparedDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || result.SchemaVersion != 1 ||
		result.Binding.AssetVersionID == uuid.Nil || !validRef(result.Binding.Source) || !digestPattern.MatchString(result.Binding.CanvasHash) ||
		!validRef(result.Proxy) || !validRef(result.Faces) {
		return CapturePreparedDocument{}, ErrInvalid
	}
	jcs, err := canonical.Marshal(result)
	if err != nil || !bytes.Equal(jcs, raw) {
		return CapturePreparedDocument{}, ErrInvalid
	}
	return result, nil
}

// Artifact resolves a child ref from the verified preparation manifest. Exact
// producer attempt/fence and adopted links are checked in SQL, not inferred
// from an object key or from a matching tenant alone.
func (s SQLCapturePreparedSource) Artifact(ctx context.Context, tenant, run uuid.UUID,
	ref Ref, role string) (objectstore.Object, error) {
	limits := map[string]int64{"working_image": 1 << 20, "faces": 8 << 20, "working_rgb": 50339840, "working_alpha": 16785408}
	limit, ok := limits[role]
	if s.DB == nil || s.Bucket == "" || tenant == uuid.Nil || run == uuid.Nil || !validRef(ref) || !ok {
		return objectstore.Object{}, ErrInvalid
	}
	var actual Ref
	var obj objectstore.Object
	var bucket string
	err := s.DB.QueryRow(ctx, `SELECT artifact_id,sha256,bytes,bucket,object_key,object_version,media_type
 FROM photo.capture_prepared_artifact($1,$2,$3,$4,$5)`, tenant, run, ref.ArtifactID, ref.SHA256, role).Scan(
		&actual.ArtifactID, &actual.SHA256, &obj.Bytes, &bucket, &obj.Key, &obj.VersionID, &obj.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return objectstore.Object{}, ErrExpired
	}
	if err != nil {
		return objectstore.Object{}, err
	}
	media := "application/json"
	if role == "working_rgb" || role == "working_alpha" {
		media = "application/octet-stream"
	}
	if actual != ref || bucket != s.Bucket || obj.Bytes < 1 || obj.Bytes > limit || obj.ContentType != media || obj.VersionID == "" || obj.VersionID == "null" {
		return objectstore.Object{}, ErrInvalid
	}
	obj.SHA256 = ref.SHA256
	return obj, nil
}

// ResolveProxy checks child provenance before exposing refs to the assessment
// closure. source must already have passed receipt-bound byte verification.
func (s SQLCapturePreparedSource) ResolveProxy(ctx context.Context, tenant, run uuid.UUID,
	prepared CapturePreparedDocument, source CaptureSourceImage) (CaptureImageInputs, error) {
	if prepared.Binding != source.Binding {
		return CaptureImageInputs{}, ErrInvalid
	}
	object, err := s.Artifact(ctx, tenant, run, prepared.Proxy, "working_image")
	if err != nil {
		return CaptureImageInputs{}, err
	}
	proxy, err := ReadVerifiedWorkingImage(ctx, s.Objects, object, prepared.Proxy)
	if err != nil {
		return CaptureImageInputs{}, err
	}
	if proxy.Canvas.SourceSHA256 != source.OriginalSHA || proxy.Canvas.Width > 2048 || proxy.Canvas.Height > 2048 ||
		proxy.Color != source.Image.Color || (proxy.Alpha == nil) != (source.Image.Alpha == nil) {
		return CaptureImageInputs{}, ErrInvalid
	}
	pixels := Ref{ArtifactID: proxy.Pixels.ID, SHA256: proxy.Pixels.SHA256}
	if _, err = s.Artifact(ctx, tenant, run, pixels, "working_rgb"); err != nil {
		return CaptureImageInputs{}, err
	}
	result := CaptureImageInputs{Pixels: pixels}
	if proxy.Alpha != nil {
		alpha := Ref{ArtifactID: proxy.Alpha.ID, SHA256: proxy.Alpha.SHA256}
		if _, err = s.Artifact(ctx, tenant, run, alpha, "working_alpha"); err != nil {
			return CaptureImageInputs{}, err
		}
		result.Alpha = &alpha
	}
	if _, err = s.Artifact(ctx, tenant, run, prepared.Faces, "faces"); err != nil {
		return CaptureImageInputs{}, err
	}
	return result, nil
}
