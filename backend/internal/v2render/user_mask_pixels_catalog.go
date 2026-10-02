package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// UserMaskPixelsCatalog proves new user-upload lineage before a MaskRef exists.
// It is distinct from adopted-mask access, which requires its exact parent.
type UserMaskPixelsCatalog interface {
	LookupUserMaskPixels(context.Context, auth.Identity, uuid.UUID, uuid.UUID, v2input.Ref, int64, int64) (objectstore.Object, error)
}
type UserMaskPublicationCatalog interface {
	UserMaskPixelsCatalog
	BindUserMask(context.Context, auth.Identity, uuid.UUID, v2input.SessionVersionBinding, v2input.Ref, []byte) error
}
type SQLUserMaskPixelsCatalog struct {
	DB     *pgxpool.Pool
	Bucket string
}

func (s SQLUserMaskPixelsCatalog) LookupUserMaskPixels(ctx context.Context, who auth.Identity, tid, version uuid.UUID, ref v2input.Ref, width, height int64) (objectstore.Object, error) {
	var object objectstore.Object
	if err := ctx.Err(); err != nil {
		return object, err
	}
	if s.DB == nil || s.Bucket == "" {
		return object, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if tid == uuid.Nil || version == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !validLayerArtifactRef(ref) || width < 1 || width > 200000000 || height < 1 || height > 200000000/width {
		return object, v2input.ErrInvalid
	}
	var bucket string
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, `SELECT bytes,bucket,object_key,object_version,media_type,sha256 FROM photo.read_v25_user_mask_pixels($1,$2,$3,$4,$5,$6,$7,$8)`, tid, version, who.UserID, who.SessionID, ref.ArtifactID, ref.SHA256, width, height).Scan(&object.Bytes, &bucket, &object.Key, &object.VersionID, &object.ContentType, &object.SHA256)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	if err != nil {
		return objectstore.Object{}, err
	}
	if bucket != s.Bucket {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return object, nil
}

func (s SQLUserMaskPixelsCatalog) BindUserMask(ctx context.Context, who auth.Identity, tid uuid.UUID, base v2input.SessionVersionBinding, root v2input.Ref, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || s.Bucket == "" {
		return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if tid == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || base.AssetVersionID == uuid.Nil || !validLayerArtifactRef(base.Source) || !validLayerArtifactRef(root) || len(raw) < 2 || len(raw) > 1048576 || maskSHAProduction(raw) != root.SHA256 {
		return v2input.ErrInvalid
	}
	var bound bool
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, `SELECT photo.bind_v25_user_mask($1,$2,$3,$4,$5,$6,$7,$8,$9)`, tid, base.AssetVersionID, who.UserID, who.SessionID, root.ArtifactID, string(raw), base.Source.ArtifactID, base.Source.SHA256, base.CanvasHash).Scan(&bound)
	})
	if err != nil {
		return err
	}
	if !bound {
		return v2input.ErrInvalid
	}
	return nil
}
func maskSHAProduction(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
