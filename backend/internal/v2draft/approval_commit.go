package v2draft

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type ApprovalRequest struct {
	ExpectedRevision string    `json:"expected_revision"`
	DocumentID       uuid.UUID `json:"document_id"`
}

func nullLearningOrdinal(version *uuid.UUID, ordinal int) any {
	if version == nil {
		return nil
	}
	return ordinal
}

// commitResolvedApproval is called only after its resolver has verified every
// dependency in ResolvedRecipeInputs. It stages deterministic FrozenRecipe
// bytes, then commits the pointer, approval row, audit and idempotent response
// in one database transaction. A concurrent replace is rejected by SQL CAS.
func (s Service) commitResolvedApproval(ctx context.Context, who auth.Identity,
	tenantID, assetID uuid.UUID, key string, request ApprovalRequest,
	draft ApprovalDraft, inputs ResolvedRecipeInputs) (idempotency.Response, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return idempotency.Response{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if !idempotency.ValidKey(key) {
		return idempotency.Response{}, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	expected, err := replaceRevision(request.ExpectedRevision)
	if err != nil || expected < 1 || request.DocumentID == uuid.Nil ||
		assetID == uuid.Nil || draft.AssetVersionID == uuid.Nil ||
		draft.DocumentID != request.DocumentID || draft.Binding != inputs.Binding ||
		inputs.ICCLockSHA != draft.Image.Color.ICCSHA256 {
		return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if draft.Revision != expected && !(draft.Approved && draft.Revision > 1 &&
		draft.Revision-1 == expected) {
		return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
	}
	frozen, err := ResolveRecipe(draft.Recipe, inputs)
	if err != nil {
		if errors.Is(err, v2input.ErrInvalid) {
			return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
		}
		return idempotency.Response{}, err
	}
	var learningDependency struct {
		VersionID *uuid.UUID `json:"learning_version_id"`
	}
	if json.Unmarshal(draft.Recipe, &learningDependency) != nil {
		return idempotency.Response{}, domain.NewError("DOCUMENT_INVALID", 422)
	}
	if learningDependency.VersionID == nil {
		if inputs.LearningModelRef != (v2input.Ref{}) ||
			inputs.LearningDescriptorRef != (v2input.Ref{}) ||
			inputs.LearningAnalysisID != uuid.Nil ||
			inputs.LearningDescriptorOrdinal != 0 {
			return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
		}
	} else if !isDocumentRef(inputs.LearningModelRef) ||
		!isDocumentRef(inputs.LearningDescriptorRef) ||
		inputs.LearningAnalysisID == uuid.Nil ||
		inputs.LearningDescriptorOrdinal < 0 ||
		inputs.LearningDescriptorOrdinal > 4999 {
		return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	frozenID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("v2-frozen/"+
		tenantID.String()+"/"+request.DocumentID.String()))
	ref, err := (Publisher{DB: s.DB, Objects: s.Objects,
		Bucket: s.Bucket}).Publish(ctx, who, tenantID, draft.AssetVersionID,
		frozenID, frozen)
	if err != nil {
		return idempotency.Response{}, draftMutationError(err)
	}
	var response idempotency.Response
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor,
		func(tx pgx.Tx, _ domain.Role) error {
			var e error
			response, e = idempotency.Use(ctx, tx, idempotency.Scope{
				Tenant: tenantID, Principal: who.UserID, PrincipalType: "user",
				Route: "Draft.Approve." + assetID.String(), Key: key,
			}, request, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
				var body json.RawMessage
				e := tx.QueryRow(ctx, `SELECT photo.approve_v23_draft_with_sources(
				 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, tenantID, draft.AssetVersionID,
					expected, request.DocumentID, who.UserID, who.SessionID,
					ref.ArtifactID, ref.SHA256,
					learningDependency.VersionID,
					nullUUID(inputs.LearningModelRef.ArtifactID),
					nullString(inputs.LearningModelRef.SHA256),
					nullUUID(inputs.LearningDescriptorRef.ArtifactID),
					nullString(inputs.LearningDescriptorRef.SHA256),
					nullUUID(inputs.LearningAnalysisID),
					nullLearningOrdinal(learningDependency.VersionID,
						inputs.LearningDescriptorOrdinal)).Scan(&body)
				if e != nil {
					return idempotency.Response{}, e
				}
				return idempotency.Response{Status: 200, Body: body,
					ResourceID: &draft.StreamID}, nil
			})
			return e
		})
	return response, draftMutationError(err)
}
