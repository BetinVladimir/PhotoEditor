package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type DefectContextRefs struct{ Resolved, Report review.ArtifactRef }
type ExpansionArtifact struct {
	Object         objectstore.Object
	Role, Producer string
	Canonical      bool
}
type ArtifactOpener interface {
	Open(context.Context, objectstore.Object) (io.ReadCloser, error)
}
type ExpansionArtifacts struct {
	DB      artifactjson.RowQuerier
	Objects ArtifactOpener
	Bucket  string
}

func validExpansionRef(ref review.ArtifactRef) bool {
	_, err := domain.ParseDigest(ref.SHA256)
	return ref.ID != uuid.Nil && err == nil
}
func (a ExpansionArtifacts) Context(ctx context.Context, lease DefectExpansionLease) (DefectContextRefs, error) {
	var refs DefectContextRefs
	if a.DB == nil || lease.ExpansionID == uuid.Nil || lease.Token == uuid.Nil {
		return refs, objectstore.ErrInvalid
	}
	err := a.DB.QueryRow(ctx, "SELECT resolved_id,resolved_sha,report_id,report_sha FROM photo.defect_expansion_context($1,$2,$3)", lease.ExpansionID, lease.Token, lease.DecisionHash).Scan(&refs.Resolved.ID, &refs.Resolved.SHA256, &refs.Report.ID, &refs.Report.SHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefectContextRefs{}, objectstore.ErrMissing
	}
	if err != nil {
		return DefectContextRefs{}, err
	}
	if !validExpansionRef(refs.Resolved) || !validExpansionRef(refs.Report) {
		return DefectContextRefs{}, objectstore.ErrIntegrity
	}
	return refs, nil
}
func (a ExpansionArtifacts) Lookup(ctx context.Context, lease DefectExpansionLease, ref review.ArtifactRef) (ExpansionArtifact, error) {
	var out ExpansionArtifact
	var id uuid.UUID
	var bucket string
	var role, producer *string
	if a.DB == nil || a.Bucket == "" || lease.ExpansionID == uuid.Nil || lease.Token == uuid.Nil || !validExpansionRef(ref) {
		return out, objectstore.ErrInvalid
	}
	err := a.DB.QueryRow(ctx, `SELECT artifact_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,producer_operation,is_canonical FROM photo.expansion_artifact($1,$2,$3,$4)`, lease.ExpansionID, lease.Token, ref.ID, ref.SHA256).Scan(
		&id, &bucket, &out.Object.Key, &out.Object.VersionID, &out.Object.SHA256, &out.Object.Bytes, &out.Object.ContentType, &role, &producer, &out.Canonical)
	if errors.Is(err, pgx.ErrNoRows) {
		return ExpansionArtifact{}, objectstore.ErrMissing
	}
	if err != nil {
		return ExpansionArtifact{}, err
	}
	if id != ref.ID || bucket != a.Bucket || out.Object.SHA256 != ref.SHA256 || out.Object.VersionID == "" || out.Object.VersionID == "null" || out.Object.Bytes < 0 {
		return ExpansionArtifact{}, objectstore.ErrIntegrity
	}
	if role != nil {
		out.Role = *role
	}
	if producer != nil {
		out.Producer = *producer
	}
	return out, nil
}

// JSON verifies the entire bounded object before exposing bytes. Typed semantic
// validation and required producer roles are enforced by the source loader.
func (a ExpansionArtifacts) JSON(ctx context.Context, lease DefectExpansionLease, ref review.ArtifactRef) ([]byte, ExpansionArtifact, error) {
	if a.Objects == nil {
		return nil, ExpansionArtifact{}, objectstore.ErrInvalid
	}
	meta, err := a.Lookup(ctx, lease, ref)
	if err != nil {
		return nil, ExpansionArtifact{}, err
	}
	if meta.Object.ContentType != "application/json" || meta.Object.Bytes > 1048576 {
		return nil, ExpansionArtifact{}, objectstore.ErrInvalid
	}
	reader, err := a.Objects.Open(ctx, meta.Object)
	if err != nil {
		return nil, ExpansionArtifact{}, err
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, 1048577))
	if err != nil {
		return nil, ExpansionArtifact{}, err
	}
	if err = ctx.Err(); err != nil {
		return nil, ExpansionArtifact{}, err
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != meta.Object.Bytes || len(raw) > 1048576 || hex.EncodeToString(sum[:]) != ref.SHA256 {
		return nil, ExpansionArtifact{}, objectstore.ErrIntegrity
	}
	if _, err = canonical.Bytes(raw); err != nil {
		return nil, ExpansionArtifact{}, objectstore.ErrIntegrity
	}
	return raw, meta, nil
}
