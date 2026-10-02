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
	"photoeditor/backend/internal/objectstore"
)

type SQLCaptureShotSource struct {
	DB      AnalysisStageQuerier
	Objects SessionAnalysisObjectReader
	Bucket  string
}

// Load returns a typed empty list when selection explicitly had no ShotList.
// The caller publishes that empty body as an immutable context dependency.
func (s SQLCaptureShotSource) Load(ctx context.Context, lease CaptureAssessmentLease, seed CaptureSelectionSeed) (CaptureShotList, error) {
	empty := CaptureShotList{1, []CaptureShotRequirement{}}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || !validCaptureLease(lease) || seed.SessionID != lease.SessionID {
		return empty, ErrInvalid
	}
	raw, err := json.Marshal(seed)
	if err != nil {
		return empty, err
	}
	if _, err = CompileCaptureSelection(raw, lease.SessionID); err != nil {
		return empty, err
	}
	if seed.ShotDocument == nil {
		return empty, nil
	}
	var document uuid.UUID
	var ref Ref
	var obj objectstore.Object
	var bucket string
	err = s.DB.QueryRow(ctx, `SELECT document_id,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.capture_shot_source($1,$2,$3)`, lease.TenantID, lease.JobID, lease.Token).Scan(&document, &ref.ArtifactID, &ref.SHA256, &obj.Bytes, &bucket, &obj.Key, &obj.VersionID, &obj.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrExpired
	}
	if err != nil {
		return empty, err
	}
	if bucket != s.Bucket || document != seed.ShotDocument.DocumentID || ref != (Ref{seed.ShotDocument.ArtifactID, seed.ShotDocument.SHA256}) {
		return empty, ErrInvalid
	}
	obj.SHA256 = ref.SHA256
	return ReadCaptureShotList(ctx, s.Objects, obj, ref)
}

func ReadCaptureShotList(ctx context.Context, objects SessionAnalysisObjectReader, obj objectstore.Object, ref Ref) (CaptureShotList, error) {
	var shots CaptureShotList
	if objects == nil || !validRef(ref) || obj.SHA256 != ref.SHA256 || obj.Bytes < 2 || obj.Bytes > 1048576 || obj.VersionID == "" || obj.VersionID == "null" || obj.ContentType != "application/json" {
		return shots, ErrInvalid
	}
	reader, err := objects.Open(ctx, obj)
	if err != nil {
		return shots, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, 1048577))
	closeErr := reader.Close()
	if readErr != nil {
		return shots, readErr
	}
	sum := sha256.Sum256(raw)
	if closeErr != nil || ctx.Err() != nil || int64(len(raw)) != obj.Bytes || hex.EncodeToString(sum[:]) != ref.SHA256 {
		return shots, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&shots) != nil || decoder.Decode(new(any)) != io.EOF {
		return shots, ErrInvalid
	}
	canonicalBody, err := CompileCaptureShotList(shots)
	if err != nil || !bytes.Equal(canonicalBody, raw) {
		return shots, ErrInvalid
	}
	return shots, nil
}
