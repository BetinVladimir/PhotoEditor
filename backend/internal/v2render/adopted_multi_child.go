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
	"strconv"
	"strings"
)

func multiChildObjectLimit(path, contract string) (string, int64, bool) {
	switch contract {
	case "WorkingImage":
		if path == "$/working_image" {
			return "application/json", 1048576, true
		}
	case "ImageReport":
		if path == "$/report" {
			return "application/json", 65536, true
		}
	case "MaskRef":
		if path == "$/changed_mask" {
			return "application/json", 1048576, true
		}
	case "Artifact":
		if path == "$/report/registrations/*" {
			return "application/json", 65536, true
		}
	case "WorkingPixels":
		if path == "$/working_image/pixels" {
			return "application/x-npy", 2400008192, true
		}
	case "WorkingAlpha":
		if path == "$/working_image/alpha" {
			return "application/x-npy", 800008192, true
		}
	case "MaskPixels":
		if path == "$/changed_mask/artifact" {
			return "application/x-npy", 800008192, true
		}
	case "DenseWarpField":
		if path == "$/working_image/transform_chain/steps/*/inverse_field" || path == "$/changed_mask/transform_chain/steps/*/inverse_field" || path == "$/report/registrations/*/inverse_field" {
			return "application/x-npy", 1600008192, true
		}
	}
	return "", 0, false
}
func multiOriginalObjectLimit(role string) (string, int64, bool, bool) {
	switch role {
	case "WorkingImage":
		return "application/json", 1048576, false, true
	case "WorkingPixels":
		return "application/x-npy", 2400008192, true, true
	case "WorkingAlpha":
		return "application/x-npy", 800008192, true, true
	}
	if strings.HasPrefix(role, "InverseField/") {
		value := strings.TrimPrefix(role, "InverseField/")
		step, err := strconv.Atoi(value)
		if err == nil && step >= 0 && step < 128 && strconv.Itoa(step) == value {
			return "application/x-npy", 1600008192, false, true
		}
	}
	return "", 0, false, false
}
func (s SQLAdoptedMultiSource) validScope(who auth.Identity, tenant, project, producer uuid.UUID, operation string, root, child v2input.Ref, bucket string) bool {
	return s.DB != nil && who.UserID != uuid.Nil && who.SessionID != uuid.Nil && tenant != uuid.Nil && project != uuid.Nil && producer != uuid.Nil && (operation == "optical.panorama" || operation == "optical.focus_stack") && validLayerArtifactRef(root) && validLayerArtifactRef(child) && root.ArtifactID != child.ArtifactID && bucket != ""
}

// LoadChildObject grants only an exact produced output occurrence, not its bytes
// or geometry. Every call repeats root/current-original authority through SQL.
func (s SQLAdoptedMultiSource) LoadChildObject(ctx context.Context, who auth.Identity, tenantID, project, producer uuid.UUID, operation string, root, child v2input.Ref, path, contract, bucket string) (objectstore.Object, error) {
	media, limit, allowed := multiChildObjectLimit(path, contract)
	if !allowed || !s.validScope(who, tenantID, project, producer, operation, root, child, bucket) {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return s.multiObjectMetadata(ctx, who, tenantID, bucket, child, media, limit, false,
		`SELECT bucket,object_key,object_version,bytes,media_type FROM photo.v29_adopted_multi_child_object($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		tenantID, project, producer, who.UserID, who.SessionID, operation, root.ArtifactID, root.SHA256, child.ArtifactID, child.SHA256, path, contract)
}

// Original input authority stays separate from adopted output membership. The
// ordinal and literal manifest role must identify this exact input occurrence.
func (s SQLAdoptedMultiSource) LoadOriginalObject(ctx context.Context, who auth.Identity, tenantID, project, producer uuid.UUID, operation string, root, child v2input.Ref, ordinal int, role, bucket string) (objectstore.Object, error) {
	media, limit, legacy, allowed := multiOriginalObjectLimit(role)
	count := 30
	if operation == "optical.panorama" {
		count = 20
	}
	if !allowed || ordinal < 0 || ordinal >= count || !s.validScope(who, tenantID, project, producer, operation, root, child, bucket) {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return s.multiObjectMetadata(ctx, who, tenantID, bucket, child, media, limit, legacy,
		`SELECT bucket,object_key,object_version,bytes,media_type FROM photo.v29_adopted_multi_original_object($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		tenantID, project, producer, who.UserID, who.SessionID, operation, root.ArtifactID, root.SHA256, ordinal, child.ArtifactID, child.SHA256, role)
}
func (s SQLAdoptedMultiSource) multiObjectMetadata(ctx context.Context, who auth.Identity, tenantID uuid.UUID, bucket string, child v2input.Ref, media string, limit int64, legacy bool, query string, args ...any) (objectstore.Object, error) {
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	var obj objectstore.Object
	var actualBucket string
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_api" {
			return v2input.ErrInvalid
		}
		err := tx.QueryRow(ctx, query, args...).Scan(&actualBucket, &obj.Key, &obj.VersionID, &obj.Bytes, &obj.ContentType)
		if errors.Is(err, pgx.ErrNoRows) {
			return v2input.ErrExpired
		}
		return err
	})
	if err != nil {
		return objectstore.Object{}, err
	}
	obj.SHA256 = child.SHA256
	if actualBucket != bucket || !validMultiChildObject(obj, child, media, limit, legacy) {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return obj, nil
}
func validMultiChildObject(obj objectstore.Object, ref v2input.Ref, media string, limit int64, legacy bool) bool {
	return obj.Key != "" && obj.VersionID != "" && obj.VersionID != "null" && obj.SHA256 == ref.SHA256 && obj.Bytes >= 1 && obj.Bytes <= limit && (obj.ContentType == media || legacy && obj.ContentType == "application/octet-stream")
}
