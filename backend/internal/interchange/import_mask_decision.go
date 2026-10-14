package interchange

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/editor"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type ImportPersonMaskDecisionRequest struct {
	Action             string    `json:"action"`
	EntryID            uuid.UUID `json:"entry_id"`
	TargetVersionID    uuid.UUID `json:"target_version_id"`
	OverrideArtifactID uuid.UUID `json:"override_artifact_id"`
	MaskArtifactID     uuid.UUID `json:"mask_artifact_id"`
	ParsedJCS          string    `json:"parsed_jcs,omitempty"`
	RecipeArtifactID   uuid.UUID `json:"recipe_artifact_id,omitempty"`
	RecipeJCS          string    `json:"recipe_jcs,omitempty"`
	OverrideJCS        string    `json:"override_jcs,omitempty"`
	MaskJCS            string    `json:"mask_jcs,omitempty"`
}

type ImportPersonMaskDecisionReceipt struct {
	WorkflowID uuid.UUID `json:"workflow_id"`
	EntryID    uuid.UUID `json:"entry_id"`
	Status     string    `json:"status"`
	Epoch      int64     `json:"epoch"`
}

type ImportPersonMaskDecisionService struct{ DB *pgxpool.Pool }

func canonicalImportDecisionDocument(raw string, limit int) bool {
	if len(raw) < 2 || len(raw) > limit {
		return false
	}
	normalized, err := canonical.Bytes([]byte(raw))
	return err == nil && bytes.Equal(normalized, []byte(raw))
}

func validateImportPersonMaskDecision(ctx context.Context, req ImportPersonMaskDecisionRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if req.EntryID == uuid.Nil || req.TargetVersionID == uuid.Nil || req.OverrideArtifactID == uuid.Nil || req.MaskArtifactID == uuid.Nil {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	if req.Action == "revoke" {
		if req.ParsedJCS != "" || req.RecipeArtifactID != uuid.Nil || req.RecipeJCS != "" || req.OverrideJCS != "" || req.MaskJCS != "" {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
		return nil
	}
	if req.Action != "grant" || req.RecipeArtifactID == uuid.Nil ||
		!canonicalImportDecisionDocument(req.ParsedJCS, 8<<20) ||
		!canonicalImportDecisionDocument(req.RecipeJCS, 1<<20) ||
		!canonicalImportDecisionDocument(req.OverrideJCS, 8<<20) ||
		!canonicalImportDecisionDocument(req.MaskJCS, 1<<20) {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	if _, err := validateParsedImportDocument(ctx, []byte(req.ParsedJCS)); err != nil {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	var recipe struct {
		Source v2input.SessionVersionBinding `json:"source"`
	}
	if json.Unmarshal([]byte(req.RecipeJCS), &recipe) != nil || v2input.ValidateEditingRecipeV3([]byte(req.RecipeJCS), recipe.Source) != nil {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	mask, err := review.ParseMaskRef([]byte(req.MaskJCS))
	if err != nil || mask.Selector != "person" || mask.ObjectID != nil || mask.ModelLockID == nil || mask.Artifact.ID != req.MaskArtifactID {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	if err := editor.ValidateOverride([]byte(req.OverrideJCS), mask.Canvas.SourceSHA256); err != nil {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	return ctx.Err()
}

func (s ImportPersonMaskDecisionService) Decide(ctx context.Context, who auth.Identity, tenant, project, workflow uuid.UUID, req ImportPersonMaskDecisionRequest) (ImportPersonMaskDecisionReceipt, error) {
	var zero ImportPersonMaskDecisionReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, domain.NewError("INTERCHANGE_UNAVAILABLE", 503)
	}
	if tenant == uuid.Nil || project == uuid.Nil || workflow == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, domain.NewError("VALIDATION_FAILED", 422)
	}
	if err := validateImportPersonMaskDecision(ctx, req); err != nil {
		return zero, err
	}
	var epoch *int64
	err := database.TenantTransaction(ctx, s.DB, tenant, func(tx pgx.Tx) error {
		var actualProject uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT project_id FROM photo.v2_workflows WHERE tenant_id=$1 AND id=$2 AND workflow_type='interchange'`, tenant, workflow).Scan(&actualProject); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewError("NOT_FOUND", 404)
			}
			return err
		}
		if actualProject != project {
			return domain.NewError("NOT_FOUND", 404)
		}
		if req.Action == "grant" {
			return tx.QueryRow(ctx, `SELECT photo.grant_v30_import_person_mask($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
				tenant, who.UserID, who.SessionID, workflow, req.EntryID,
				req.ParsedJCS, req.RecipeArtifactID, req.RecipeJCS, req.OverrideArtifactID,
				req.OverrideJCS, req.MaskJCS, req.TargetVersionID).Scan(&epoch)
		}
		return tx.QueryRow(ctx, `SELECT photo.revoke_v30_import_person_mask($1,$2,$3,$4,$5,$6,$7,$8)`,
			tenant, who.UserID, who.SessionID, workflow, req.EntryID,
			req.OverrideArtifactID, req.MaskArtifactID, req.TargetVersionID).Scan(&epoch)
	})
	if err != nil {
		return zero, err
	}
	if epoch == nil || *epoch < 1 {
		return zero, domain.NewError("NOT_FOUND", 404)
	}
	status := "granted"
	if req.Action == "revoke" {
		status = "revoked"
	}
	return ImportPersonMaskDecisionReceipt{WorkflowID: workflow, EntryID: req.EntryID, Status: status, Epoch: *epoch}, ctx.Err()
}
