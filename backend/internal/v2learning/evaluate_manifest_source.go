package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
)

type LearningEvaluateObjectReader interface {
	Open(context.Context, objectstore.Object) (io.ReadCloser, error)
}

// SQLLearningEvaluateManifestSource reads the exact versioned manifest that
// the frozen workflow snapshot and live evaluation lease both reference.
type SQLLearningEvaluateManifestSource struct {
	DB      *pgxpool.Pool
	Objects LearningEvaluateObjectReader
	Bucket  string
}

// LearningEvaluateRenderResolver verifies the immutable nested WorkingImage,
// FrozenRecipe, MaskRef and StyleV2 object versions for every holdout event.
type LearningEvaluateRenderResolver interface {
	Resolve(context.Context, LearningEvaluateLease,
		TrainingManifest) ([]LearningRenderInputs, error)
}

type FrozenLearningEvaluateSource struct {
	Manifests SQLLearningEvaluateManifestSource
	Renders   LearningEvaluateRenderResolver
}

func (s FrozenLearningEvaluateSource) Load(ctx context.Context,
	lease LearningEvaluateLease) (TrainingDraftPrepared, []LearningRenderInputs, error) {
	if s.Renders == nil {
		return TrainingDraftPrepared{}, nil, ErrInvalidTrainingDraft
	}
	manifest, manifestSHA, err := s.Manifests.LoadManifest(ctx, lease)
	if err != nil {
		return TrainingDraftPrepared{}, nil, err
	}
	resolved, err := s.Renders.Resolve(ctx, lease, manifest)
	if err != nil {
		return TrainingDraftPrepared{}, nil, err
	}
	return TrainingDraftPrepared{TenantID: lease.TenantID,
		RunID: lease.RunID, WorkflowID: lease.WorkflowID,
		Manifest: manifest, ManifestSHA: manifestSHA}, resolved, nil
}

func (s SQLLearningEvaluateManifestSource) LoadManifest(ctx context.Context,
	lease LearningEvaluateLease) (TrainingManifest, string, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" ||
		!validLearningEvaluateLease(lease) {
		return TrainingManifest{}, "", ErrInvalidTrainingDraft
	}
	var object objectstore.Object
	var bucket string
	var artifactID uuid.UUID
	err := s.DB.QueryRow(ctx, `SELECT artifact_id,sha256,bytes,bucket,
	 object_key,object_version,media_type
	 FROM photo.v23_evaluate_manifest_source($1,$2,$3,$4)`,
		lease.TenantID, lease.RunID, lease.ExpansionID,
		lease.Token).Scan(&artifactID, &object.SHA256, &object.Bytes,
		&bucket, &object.Key, &object.VersionID, &object.ContentType)
	if err == pgx.ErrNoRows {
		return TrainingManifest{}, "", ErrTrainingDraftConflict
	}
	if err != nil {
		return TrainingManifest{}, "", err
	}
	expectedID, err := TrainingDocumentID(lease.RunID, trainingManifestPurpose)
	if err != nil || artifactID != expectedID {
		return TrainingManifest{}, "", ErrInvalidTrainingDraft
	}
	return ReadFrozenEvaluationManifest(ctx, s.Objects, object, bucket,
		s.Bucket)
}

// ReadFrozenEvaluationManifest rejects changed or noncanonical object bytes.
func ReadFrozenEvaluationManifest(ctx context.Context,
	reader LearningEvaluateObjectReader, object objectstore.Object,
	bucket, expectedBucket string) (TrainingManifest, string, error) {
	if err := ctx.Err(); err != nil {
		return TrainingManifest{}, "", err
	}
	if reader == nil || bucket != expectedBucket || bucket == "" ||
		object.Key == "" || object.VersionID == "" || object.VersionID == "null" ||
		object.ContentType != "application/json" || object.Bytes < 2 ||
		object.Bytes > 8388608 || !validSHA(object.SHA256) {
		return TrainingManifest{}, "", ErrInvalidTrainingDraft
	}
	stream, err := reader.Open(ctx, object)
	if err != nil {
		if stream != nil {
			_ = stream.Close()
		}
		return TrainingManifest{}, "", err
	}
	if stream == nil {
		return TrainingManifest{}, "", ErrInvalidTrainingDraft
	}
	body, readErr := io.ReadAll(io.LimitReader(stream, 8388609))
	closeErr := stream.Close()
	if readErr != nil {
		return TrainingManifest{}, "", readErr
	}
	if closeErr != nil {
		return TrainingManifest{}, "", closeErr
	}
	if err := ctx.Err(); err != nil {
		return TrainingManifest{}, "", err
	}
	if int64(len(body)) != object.Bytes {
		return TrainingManifest{}, "", ErrInvalidTrainingDraft
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != object.SHA256 {
		return TrainingManifest{}, "", ErrInvalidTrainingDraft
	}
	canonicalBody, err := canonical.Bytes(body)
	if err != nil || string(canonicalBody) != string(body) {
		return TrainingManifest{}, "", ErrInvalidTrainingDraft
	}
	var manifest TrainingManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return TrainingManifest{}, "", ErrInvalidTrainingDraft
	}
	serialized, err := canonical.Marshal(manifest)
	if err != nil || string(serialized) != string(body) ||
		!validFrozenEvaluationManifest(manifest) {
		return TrainingManifest{}, "", ErrInvalidTrainingDraft
	}
	return manifest, object.SHA256, nil
}


// This is structural frozen-policy authority only; live consent remains SQL-owned.
func validFrozenEvaluationManifest(manifest TrainingManifest) bool {
	if !validateDraftManifest(manifest) {
		return false
	}
	policy := LearningPolicy{SchemaVersion: 1, Mode: manifest.Mode,
		MinExamples: 40, MinSessions: 6, HoldoutFraction: .2, Seed: 2026,
		Calibration: manifest.Calibration}
	sha, err := canonical.Hash(policy)
	return err == nil && sha == manifest.PolicySHA
}
