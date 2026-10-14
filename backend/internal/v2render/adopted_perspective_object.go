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
)

// LoadObject returns immutable root metadata only after repeated source proof.
// Bucket is trusted deployment configuration. No object IO occurs in the tenant
// transaction; consumers must read VersionID and verify exact length/SHA.
func (s SQLAdoptedPerspectiveSource) LoadObject(ctx context.Context, who auth.Identity, tenantID, project, version, producer uuid.UUID, expected v2input.Ref, bucket string) (AdoptedPerspectiveBinding, objectstore.Object, error) {
	if bucket == "" {
		return AdoptedPerspectiveBinding{}, objectstore.Object{}, v2input.ErrInvalid
	}
	binding, err := s.Load(ctx, who, tenantID, project, version, producer, expected)
	if err != nil {
		return AdoptedPerspectiveBinding{}, objectstore.Object{}, err
	}
	var object objectstore.Object
	var actualBucket string
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_api" {
			return v2input.ErrInvalid
		}
		err := tx.QueryRow(ctx, `SELECT bucket,object_key,object_version,bytes,media_type
   FROM photo.v29_adopted_perspective_object($1,$2,$3,$4,$5,$6,$7,$8)`, tenantID, project, version, producer, who.UserID, who.SessionID, expected.ArtifactID, expected.SHA256).Scan(&actualBucket, &object.Key, &object.VersionID, &object.Bytes, &object.ContentType)
		if errors.Is(err, pgx.ErrNoRows) {
			return v2input.ErrExpired
		}
		return err
	})
	if err != nil {
		return AdoptedPerspectiveBinding{}, objectstore.Object{}, err
	}
	object.SHA256 = expected.SHA256
	if actualBucket != bucket || object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.Bytes < 2 || object.Bytes > 1048576 || object.ContentType != "application/json" {
		return AdoptedPerspectiveBinding{}, objectstore.Object{}, v2input.ErrInvalid
	}
	return binding, object, nil
}
