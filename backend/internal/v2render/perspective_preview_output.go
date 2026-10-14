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
	"time"
)

type PerspectivePreviewSigner interface {
	SignRead(context.Context, objectstore.Object, time.Duration) (objectstore.ReadURL, error)
}
type PerspectivePreview struct {
	SchemaVersion int         `json:"schema_version"`
	Tenant        uuid.UUID   `json:"tenant_id"`
	Project       uuid.UUID   `json:"project_id"`
	Version       uuid.UUID   `json:"asset_version_id"`
	Producer      uuid.UUID   `json:"producer_task_id"`
	Result        uuid.UUID   `json:"result_id"`
	Variant       string      `json:"variant"`
	Artifact      v2input.Ref `json:"artifact"`
	Bytes         int64       `json:"bytes"`
	MediaType     string      `json:"media_type"`
	Width         int         `json:"width"`
	Height        int         `json:"height"`
	URL           string      `json:"url"`
	ExpiresAt     time.Time   `json:"expires_at"`
}
type perspectivePreviewObject struct {
	Ref           v2input.Ref
	Object        objectstore.Object
	Width, Height int
}

func (s PerspectiveProposalService) previewObject(ctx context.Context, who auth.Identity, p PerspectiveProposal, variant string) (perspectivePreviewObject, error) {
	var value perspectivePreviewObject
	err := tenant.WithTenant(ctx, s.DB, who, p.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_api" {
			return v2input.ErrInvalid
		}
		var bucket string
		err := tx.QueryRow(ctx, "SELECT * FROM photo.v29_perspective_preview_output($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", p.Tenant, p.Project, p.Base.AssetVersionID, p.Producer, who.UserID, who.SessionID, p.ResultID, p.Result.SHA256, s.SRGBSHA, variant).Scan(&value.Ref.ArtifactID, &value.Ref.SHA256, &value.Object.Bytes, &value.Object.ContentType, &bucket, &value.Object.Key, &value.Object.VersionID, &value.Width, &value.Height)
		if errors.Is(err, pgx.ErrNoRows) {
			return v2input.ErrExpired
		}
		if err != nil {
			return err
		}
		value.Object.SHA256 = value.Ref.SHA256
		return validatePerspectivePreviewObject(value, bucket, s.Bucket, variant)
	})
	return value, err
}
func validatePerspectivePreviewObject(v perspectivePreviewObject, bucket, expected, variant string) error {
	edge := 1600
	if variant == "preview2560" {
		edge = 2560
	} else if variant != "preview1600" {
		return v2input.ErrInvalid
	}
	if bucket != expected || v.Ref.ArtifactID == uuid.Nil || !validSHA256(v.Ref.SHA256) || v.Object.Bytes < 1 || v.Object.Bytes > 54525952 || v.Object.Key == "" || v.Object.VersionID == "" || v.Object.VersionID == "null" || v.Width < 1 || v.Height < 1 || v.Width > edge || v.Height > edge || (v.Object.ContentType != "image/jpeg" && v.Object.ContentType != "image/webp") {
		return v2input.ErrInvalid
	}
	return nil
}
func (s PerspectiveProposalService) Preview(ctx context.Context, who auth.Identity, tenantID, project, version, producer uuid.UUID, variant string) (PerspectivePreview, error) {
	var zero PerspectivePreview
	if s.PreviewSigner == nil || !validSHA256(s.SRGBSHA) || (variant != "preview1600" && variant != "preview2560") {
		return zero, v2input.ErrInvalid
	}
	p, err := s.Describe(ctx, who, tenantID, project, version, producer)
	if err != nil {
		return zero, err
	}
	value, err := s.previewObject(ctx, who, p, variant)
	if err != nil {
		return zero, err
	}
	signed, err := s.PreviewSigner.SignRead(ctx, value.Object, time.Minute)
	if err != nil {
		return zero, err
	}
	// Signing happens outside SQL; repeat authorization and every locator before disclosure.
	current, err := s.previewObject(ctx, who, p, variant)
	if err != nil {
		return zero, err
	}
	if current != value {
		return zero, v2input.ErrExpired
	}
	if !validVersionedPreviewURL(signed.URL, value.Object.VersionID) || !signed.ExpiresAt.After(time.Now()) {
		return zero, v2input.ErrInvalid
	}
	return PerspectivePreview{SchemaVersion: 1, Tenant: tenantID, Project: project, Version: version, Producer: producer, Result: p.ResultID, Variant: variant, Artifact: value.Ref, Bytes: value.Object.Bytes, MediaType: value.Object.ContentType, Width: value.Width, Height: value.Height, URL: signed.URL, ExpiresAt: signed.ExpiresAt}, nil
}

func validSHA256(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !((c >= 48 && c <= 57) || (c >= 97 && c <= 102)) {
			return false
		}
	}
	return true
}
