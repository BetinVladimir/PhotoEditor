package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"regexp"
)

var adoptedChildPath = regexp.MustCompile(`^\$/([A-Za-z0-9_]+|\*)(/([A-Za-z0-9_]+|\*))*$`)

func childObjectLimit(contract string) (string, int64, bool) {
	switch contract {
	case "WorkingImage", "ImageReport", "MaskRef":
		return "application/json", 8 << 20, true
	case "WorkingPixels":
		return "application/x-npy", 200000000*12 + 8192, true
	case "WorkingAlpha", "MaskPixels":
		return "application/x-npy", 200000000*4 + 8192, true
	case "DenseWarpField":
		return "application/x-npy", 200000000*8 + 8192, true
	default:
		return "", 0, false
	}
}

// LoadChildObject resolves an exact task-qualified adopted output occurrence.
// It grants neither generic workflow artifacts nor reused original inputs.
// Consumers must parse/verify bytes and geometry after this SQL transaction.
func (s SQLAdoptedLensSource) LoadChildObject(ctx context.Context, who auth.Identity, tenantID, project, version, producer uuid.UUID, root, child v2input.Ref, path, contract, bucket string) (objectstore.Object, error) {
	empty := objectstore.Object{}
	media, limit, allowed := childObjectLimit(contract)
	if s.DB == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenantID == uuid.Nil || project == uuid.Nil || version == uuid.Nil || producer == uuid.Nil || !validLayerArtifactRef(root) || !validLayerArtifactRef(child) || bucket == "" || !allowed || len(path) > 512 || !adoptedChildPath.MatchString(path) {
		return empty, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	var object objectstore.Object
	var actualBucket string
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_api" {
			return v2input.ErrInvalid
		}
		err := tx.QueryRow(ctx, `SELECT bucket,object_key,object_version,bytes,media_type
   FROM photo.v25_adopted_lens_child_object($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, tenantID, project, version, producer, who.UserID, who.SessionID, root.ArtifactID, root.SHA256, child.ArtifactID, child.SHA256, path, contract).Scan(&actualBucket, &object.Key, &object.VersionID, &object.Bytes, &object.ContentType)
		if errors.Is(err, pgx.ErrNoRows) {
			return v2input.ErrExpired
		}
		return err
	})
	if err != nil {
		return empty, err
	}
	object.SHA256 = child.SHA256
	if actualBucket != bucket || object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.Bytes < 1 || object.Bytes > limit || object.ContentType != media {
		return empty, v2input.ErrInvalid
	}
	return object, nil
}
