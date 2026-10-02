package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type ImmutableStore interface {
	PutImmutable(context.Context, string, io.Reader, int64, string, string) (objectstore.Object, error)
	RecoverImmutable(context.Context, string, int64, string, string) (objectstore.Object, error)
}

type Publisher struct {
	DB      *pgxpool.Pool
	Objects ImmutableStore
	Bucket  string
}

type reservation struct {
	ArtifactID uuid.UUID
	Token      uuid.UUID
	Ready      bool
}

// Publish makes a recipe or base checkpoint immutable before its document
// pointer can be changed. documentID is stable across retries of one save.
func (p Publisher) Publish(ctx context.Context, who auth.Identity, tenantID,
	assetVersionID, documentID uuid.UUID, raw []byte) (v2input.Ref, error) {
	return p.publish(ctx, who, tenantID, assetVersionID, documentID, raw, false)
}

// PublishShotList publishes a validated session-scoped ShotList using the same
// immutable object protocol and cleanup lifecycle as recipe publication.
func (p Publisher) PublishShotList(ctx context.Context, who auth.Identity, tenantID,
	sessionID, documentID uuid.UUID, shots v2input.CaptureShotList) (v2input.Ref, error) {
	body, err := v2input.CompileCaptureShotList(shots)
	if err != nil {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	return p.publish(ctx, who, tenantID, sessionID, documentID, body, true)
}

func (p Publisher) publish(ctx context.Context, who auth.Identity, tenantID,
	assetVersionID, documentID uuid.UUID, raw []byte, shotList bool) (v2input.Ref, error) {
	reserveSQL := `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v2_draft_upload($1,$2,$3,$4,$5,$6,$7,$8)`
	completeSQL := `SELECT photo.complete_v2_draft_upload($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	if shotList {
		reserveSQL = `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v24_shot_upload($1,$2,$3,$4,$5,$6,$7,$8)`
		completeSQL = `SELECT photo.complete_v24_shot_upload($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	}
	if p.DB == nil || p.Objects == nil || p.Bucket == "" {
		return v2input.Ref{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if tenantID == uuid.Nil || assetVersionID == uuid.Nil || documentID == uuid.Nil {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	body, err := canonical.Bytes(raw)
	if err != nil || len(body) < 2 || len(body) > 1048576 {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	var r reservation
	var outcome string
	err = tenant.WithTenant(ctx, p.DB, who, tenantID, domain.Editor,
		func(tx pgx.Tx, _ domain.Role) error {
			return tx.QueryRow(ctx, reserveSQL,
				tenantID, assetVersionID, documentID, who.UserID, who.SessionID,
				digest, len(body), p.Bucket).Scan(&outcome, &r.ArtifactID, &r.Token, &r.Ready)
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
	if r.ArtifactID == uuid.Nil || r.Token == uuid.Nil {
		return v2input.Ref{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	ref := v2input.Ref{ArtifactID: r.ArtifactID, SHA256: digest}
	if r.Ready {
		return ref, nil
	}
	key, err := objectstore.V2DraftKey(tenantID, r.ArtifactID)
	if err != nil {
		return v2input.Ref{}, err
	}
	obj, err := p.Objects.PutImmutable(ctx, key, bytes.NewReader(body), int64(len(body)),
		digest, "application/json")
	if errors.Is(err, objectstore.ErrExists) || errors.Is(err, objectstore.ErrUnavailable) {
		obj, err = p.Objects.RecoverImmutable(ctx, key, int64(len(body)), digest, "application/json")
	}
	if err != nil {
		return v2input.Ref{}, err
	}
	var completed bool
	err = tenant.WithTenant(ctx, p.DB, who, tenantID, domain.Editor,
		func(tx pgx.Tx, _ domain.Role) error {
			return tx.QueryRow(ctx, completeSQL,
				tenantID, assetVersionID, documentID, who.UserID, who.SessionID,
				r.ArtifactID, r.Token, digest, len(body), p.Bucket, obj.VersionID).Scan(&completed)
		})
	if err != nil {
		return v2input.Ref{}, err
	}
	if !completed {
		return v2input.Ref{}, domain.NewError("REVISION_CONFLICT", 409)
	}
	return ref, nil
}
