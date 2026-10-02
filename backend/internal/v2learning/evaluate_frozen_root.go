package v2learning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type FrozenRootProvenance struct {
	Role    string
	TaskID  *uuid.UUID
	Attempt *int
	Fence   *int64
}

// SQLLearningEvaluateRootSource can read only an exact ref already declared
// in the canonical fit task's immutable input closure.
type SQLLearningEvaluateRootSource struct {
	DB      *pgxpool.Pool
	Objects LearningEvaluateObjectReader
	Bucket  string
}

func (s SQLLearningEvaluateRootSource) Load(ctx context.Context,
	lease LearningEvaluateLease, ref v2input.Ref,
	contract string) ([]byte, FrozenRootProvenance, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" ||
		!validLearningEvaluateLease(lease) || !validManifestRef(ref) ||
		(contract != "WorkingImage" && contract != "FrozenRecipe" &&
			contract != "MaskRef" && contract != "FaceObservations" &&
			contract != "CandidateProfile" && contract != "Calibration" &&
			contract != "ConsentSnapshot") {
		return nil, FrozenRootProvenance{}, ErrInvalidTrainingDraft
	}
	var found v2input.Ref
	var object objectstore.Object
	var bucket, role string
	var provenance FrozenRootProvenance
	err := s.DB.QueryRow(ctx, `SELECT artifact_id,sha256,bytes,bucket,
	 object_key,object_version,media_type,output_role,created_by_task_id,
	 produced_attempt,produced_fence
	 FROM photo.v23_evaluate_frozen_root($1,$2,$3,$4,$5,$6,$7)`,
		lease.TenantID, lease.RunID, lease.ExpansionID, lease.Token,
		ref.ArtifactID, ref.SHA256, contract).Scan(&found.ArtifactID,
		&found.SHA256, &object.Bytes, &bucket, &object.Key,
		&object.VersionID, &object.ContentType, &role,
		&provenance.TaskID, &provenance.Attempt, &provenance.Fence)
	if err == pgx.ErrNoRows {
		return nil, FrozenRootProvenance{}, ErrTrainingDraftConflict
	}
	if err != nil {
		return nil, FrozenRootProvenance{}, err
	}
	if found != ref || bucket != s.Bucket {
		return nil, FrozenRootProvenance{}, ErrInvalidTrainingDraft
	}
	object.SHA256 = found.SHA256
	provenance.Role = role
	body, err := ReadFrozenRootDocument(ctx, s.Objects, object, ref)
	return body, provenance, err
}

func ReadFrozenRootDocument(ctx context.Context,
	reader LearningEvaluateObjectReader, object objectstore.Object,
	ref v2input.Ref) ([]byte, error) {
	if reader == nil || !validManifestRef(ref) || object.SHA256 != ref.SHA256 ||
		object.Key == "" || object.VersionID == "" || object.VersionID == "null" ||
		object.ContentType != "application/json" || object.Bytes < 2 ||
		object.Bytes > 8388608 {
		return nil, ErrInvalidTrainingDraft
	}
	stream, err := reader.Open(ctx, object)
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(io.LimitReader(stream, 8388609))
	closeErr := stream.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil || ctx.Err() != nil || int64(len(body)) != object.Bytes {
		return nil, ErrInvalidTrainingDraft
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != ref.SHA256 {
		return nil, ErrInvalidTrainingDraft
	}
	canonicalBody, err := canonical.Bytes(body)
	if err != nil || !bytes.Equal(canonicalBody, body) {
		return nil, ErrInvalidTrainingDraft
	}
	return body, nil
}
