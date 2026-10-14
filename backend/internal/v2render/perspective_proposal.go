package v2render

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type PerspectiveProposal struct {
	SchemaVersion  int                           `json:"schema_version"`
	Tenant         uuid.UUID                     `json:"tenant_id"`
	Project        uuid.UUID                     `json:"project_id"`
	Producer       uuid.UUID                     `json:"producer_task_id"`
	ResultID       uuid.UUID                     `json:"result_id"`
	Result         v2input.Ref                   `json:"result"`
	Base           v2input.SessionVersionBinding `json:"base"`
	WorkingImage   v2input.Ref                   `json:"working_image"`
	ChangedMask    v2input.Ref                   `json:"changed_mask"`
	Report         v2input.Ref                   `json:"report"`
	Canvas         review.Canvas                 `json:"canvas"`
	Status         string                        `json:"status"`
	RequiresReview bool                          `json:"requires_review"`
}
type PerspectiveProposalService struct {
	PreviewSigner PerspectivePreviewSigner
	SRGBSHA       string
	DB            *pgxpool.Pool
	Objects       v2input.SessionAnalysisObjectReader
	Bucket        string
}

func (s PerspectiveProposalService) Describe(ctx context.Context, who auth.Identity, tenantID, project, version, producer uuid.UUID) (PerspectiveProposal, error) {
	var zero PerspectiveProposal
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Objects == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 || tenantID == uuid.Nil || project == uuid.Nil || version == uuid.Nil || producer == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	var expected v2input.Ref
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_api" {
			return v2input.ErrInvalid
		}
		err := tx.QueryRow(ctx, "SELECT artifact_id,sha256 FROM photo.v29_adopted_perspective_binding($1,$2,$3,$4,$5,$6)", tenantID, project, version, producer, who.UserID, who.SessionID).Scan(&expected.ArtifactID, &expected.SHA256)
		if errors.Is(err, pgx.ErrNoRows) {
			return v2input.ErrExpired
		}
		return err
	})
	if err != nil {
		return zero, err
	}
	catalog, binding, err := BuildAdoptedPerspectiveCatalog(ctx, SQLAdoptedPerspectiveSource{DB: s.DB}, s.Objects, who, tenantID, project, version, producer, expected, s.Bucket)
	if err != nil {
		return zero, err
	}
	result, err := v2draft.ParseImageResult(catalog.documents[expected.ArtifactID.String()], expected)
	if err != nil {
		return zero, err
	}
	image, err := review.ParseWorkingImage(catalog.documents[result.WorkingImage.ArtifactID.String()])
	if err != nil {
		return zero, err
	}
	var report struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(catalog.documents[result.Report.ArtifactID.String()], &report) != nil || (report.Status != "pass" && report.Status != "review") {
		return zero, v2input.ErrInvalid
	}
	// A final root resolution prevents describing a revoked producer after IO.
	if _, err := catalog.Lookup(ctx, who, tenantID, v2draft.LayerRef{Ref: expected, Contract: "ImageResult", Path: "Review"}); err != nil {
		return zero, err
	}
	return PerspectiveProposal{SchemaVersion: 1, Tenant: tenantID, Project: project, Producer: producer, ResultID: binding.ResultID, Result: expected, Base: binding.Base, WorkingImage: result.WorkingImage, ChangedMask: result.ChangedMask, Report: result.Report, Canvas: image.Canvas, Status: report.Status, RequiresReview: report.Status == "review"}, nil
}
