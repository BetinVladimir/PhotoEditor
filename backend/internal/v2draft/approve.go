package v2draft

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Approve compiles the pinned draft and commits its immutable approval. A
// recipe whose model or override inputs cannot yet be resolved is never
// silently compiled without those corrections.
func (s Service) Approve(ctx context.Context, who auth.Identity, tenantID,
	assetID uuid.UUID, key string, request ApprovalRequest) (idempotency.Response, error) {
	if !idempotency.ValidKey(key) {
		return idempotency.Response{}, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	expected, err := replaceRevision(request.ExpectedRevision)
	if err != nil || expected < 1 || request.DocumentID == uuid.Nil {
		return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	draft, err := s.ReadApprovalDraft(ctx, who, tenantID, assetID,
		request.DocumentID, expected)
	if err != nil {
		return idempotency.Response{}, err
	}
	var recipe struct {
		SessionCorrections *v2input.Ref  `json:"session_corrections"`
		LearningVersionID  *uuid.UUID    `json:"learning_version_id"`
		Overrides          []v2input.Ref `json:"overrides"`
		LayerStack         *v2input.Ref  `json:"layer_stack"`
	}
	if json.Unmarshal(draft.Recipe, &recipe) != nil {
		return idempotency.Response{}, domain.NewError("DOCUMENT_INVALID", 422)
	}
	if len(recipe.Overrides) != 0 || recipe.LayerStack != nil {
		return idempotency.Response{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	resolver := ClosureResolver{Catalog: SQLArtifactCatalog{DB: s.DB,
		Bucket: s.Bucket, ProjectID: draft.ProjectID,
		AssetVersionID: draft.AssetVersionID}, Objects: s.Objects}
	style, baseRef, session, err := resolver.ReadResolvedScalars(ctx, who,
		tenantID, draft.Recipe, draft.Binding)
	if err != nil {
		return idempotency.Response{}, draftMutationError(err)
	}
	inputs := ResolvedRecipeInputs{Binding: draft.Binding, BaseStyle: baseRef,
		Style: style, Session: session, ICCLockSHA: draft.Image.Color.ICCSHA256}
	if recipe.LearningVersionID != nil {
		model, e := s.ReadEligibleLearningProfile(ctx, who, tenantID,
			draft.ProjectID, draft.DocumentID, *recipe.LearningVersionID)
		if e != nil {
			return idempotency.Response{}, e
		}
		if model.ICCLockSHA != inputs.ICCLockSHA {
			return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
		}
		frame, e := s.ReadFrameDescriptor(ctx, who, tenantID, draft.ProjectID,
			draft.Binding, model.ModelLockSHA, session)
		if e != nil {
			return idempotency.Response{}, e
		}
		prediction, e := PredictCorrection(model.Profile, frame.Features)
		if e != nil {
			return idempotency.Response{}, domain.NewError("DOCUMENT_INVALID", 422)
		}
		inputs.LearningID = *recipe.LearningVersionID
		inputs.Learning = &prediction
		inputs.LearningModelRef = model.ModelRef
		inputs.LearningDescriptorRef = frame.ShardRef
		inputs.LearningAnalysisID = frame.AnalysisID
		inputs.LearningDescriptorOrdinal = frame.Ordinal
		inputs.ModelLockSHA = model.ModelLockSHA
	}
	var modelLock string
	if session != nil || recipe.LearningVersionID == nil {
		err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor,
			func(tx pgx.Tx, _ domain.Role) error {
				if session == nil {
					e := tx.QueryRow(ctx, `SELECT model_lock_sha FROM
					 photo.resolve_v2_qualification($1,$2,'session.describe','1')`,
						tenantID, draft.ProjectID).Scan(&modelLock)
					if errors.Is(e, pgx.ErrNoRows) {
						return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
					}
					return e
				}
				e := tx.QueryRow(ctx, `SELECT sa.model_lock_sha
			 FROM photo.session_analyses sa
			 JOIN photo.artifacts a ON a.tenant_id=sa.tenant_id
			  AND a.id=sa.plan_artifact_id
			 WHERE sa.tenant_id=$1 AND sa.project_id=$2
			  AND sa.plan_artifact_id=$3 AND a.sha256=$4
			  AND a.state='ready' AND a.verified_at IS NOT NULL`,
					tenantID, draft.ProjectID, session.Ref.ArtifactID,
					session.Ref.SHA256).Scan(&modelLock)
				if errors.Is(e, pgx.ErrNoRows) {
					return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
				}
				return e
			})
		if err != nil {
			return idempotency.Response{}, draftMutationError(err)
		}
		if inputs.ModelLockSHA != "" && inputs.ModelLockSHA != modelLock {
			return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
		}
		inputs.ModelLockSHA = modelLock
	}
	return s.commitResolvedApproval(ctx, who, tenantID, assetID, key,
		request, draft, inputs)
}
