package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type UserMaskPixelsPublisher struct {
	DB      *pgxpool.Pool
	Objects v2draft.ImmutableStore
	Bucket  string
}

// Publish verifies the entire seekable NPY before reserving quota. The caller
// owns the reader. Validation and object IO occur outside SQL transactions;
// PutImmutable must enforce exact SHA/length again, protecting against changed
// source bytes between validation and upload. Geometry relative to the source
// image is checked when the typed MaskRef is published/admitted.
func (p UserMaskPixelsPublisher) Publish(ctx context.Context, who auth.Identity, tenantID, version, document uuid.UUID, reader io.ReadSeeker, size int64, sha string, width, height int64) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if p.DB == nil || p.Objects == nil || p.Bucket == "" {
		return v2input.Ref{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if tenantID == uuid.Nil || version == uuid.Nil || document == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || reader == nil {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	offset, err := reader.Seek(0, io.SeekStart)
	if err != nil {
		return v2input.Ref{}, err
	}
	if offset != 0 {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	if err := VerifyUserMaskNPY(ctx, reader, size, sha, width, height); err != nil {
		return v2input.Ref{}, err
	}
	offset, err = reader.Seek(0, io.SeekStart)
	if err != nil {
		return v2input.Ref{}, err
	}
	if offset != 0 {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	var outcome string
	var artifact, token *uuid.UUID
	var ready bool
	err = tenant.WithTenant(ctx, p.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v25_user_mask_pixels($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, tenantID, version, document, who.UserID, who.SessionID, sha, size, p.Bucket, width, height).Scan(&outcome, &artifact, &token, &ready)
	})
	if err != nil {
		return v2input.Ref{}, err
	}
	switch outcome {
	case "quota":
		return v2input.Ref{}, domain.NewError("QUOTA_EXCEEDED", 409)
	case "conflict", "stale":
		return v2input.Ref{}, domain.NewError("REVISION_CONFLICT", 409)
	case "forbidden":
		return v2input.Ref{}, domain.NewError("FORBIDDEN", 403)
	case "created", "resumed", "replayed":
	default:
		return v2input.Ref{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if artifact == nil || token == nil || *artifact == uuid.Nil || *token == uuid.Nil {
		return v2input.Ref{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	ref := v2input.Ref{ArtifactID: *artifact, SHA256: sha}
	if ready {
		return ref, nil
	}
	key, err := objectstore.V2DraftKey(tenantID, *artifact)
	if err != nil {
		return v2input.Ref{}, err
	}
	object, err := p.Objects.PutImmutable(ctx, key, reader, size, sha, "application/x-npy")
	if errors.Is(err, objectstore.ErrExists) || errors.Is(err, objectstore.ErrUnavailable) {
		object, err = p.Objects.RecoverImmutable(ctx, key, size, sha, "application/x-npy")
	}
	if err != nil {
		return v2input.Ref{}, err
	}
	if object.Key != key || object.Bytes != size || object.SHA256 != sha || object.ContentType != "application/x-npy" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	var complete bool
	err = tenant.WithTenant(ctx, p.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, `SELECT photo.complete_v25_user_mask_pixels($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, tenantID, version, document, who.UserID, who.SessionID, *artifact, *token, sha, size, p.Bucket, object.VersionID, width, height).Scan(&complete)
	})
	if err != nil {
		return v2input.Ref{}, err
	}
	if !complete {
		return v2input.Ref{}, domain.NewError("REVISION_CONFLICT", 409)
	}
	return ref, nil
}
