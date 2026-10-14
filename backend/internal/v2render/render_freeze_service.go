package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"time"
)

type RenderFreezeQualificationResolver interface {
	Resolve(context.Context, auth.Identity, uuid.UUID, uuid.UUID) (RenderQualification, error)
}
type SQLRenderFreezeService struct {
	Drafts        v2draft.Service
	Qualification RenderFreezeQualificationResolver
}

// Freeze performs object IO before the final atomic registration/idempotency
// transaction. Completed retries reauthorize scope without reading objects or
// recomputing a changed qualification or learning model.
func (s SQLRenderFreezeService) Freeze(ctx context.Context, who auth.Identity, tid, asset uuid.UUID, key string, request RenderFreezeRequest) (idempotency.Response, error) {
	if err := ctx.Err(); err != nil {
		return idempotency.Response{}, err
	}
	if request.AssumeProfile != nil {
		value := *request.AssumeProfile
		request.AssumeProfile = &value
	}
	id, err := RenderFreezeDocumentID(tid, who.UserID, asset, key, request)
	if err != nil || who.SessionID == uuid.Nil {
		return idempotency.Response{}, v2input.ErrInvalid
	}
	expected, _ := renderFreezeRevision(request)
	if s.Drafts.DB == nil {
		return idempotency.Response{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	scope := idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Render.Freeze." + asset.String(), Key: key}
	var pending []byte
	saved, found, err := s.readFreezeReplay(ctx, who, asset, scope, request, id, &pending)
	if err != nil || found {
		return saved, err
	}
	if s.Drafts.Objects == nil || s.Drafts.Bucket == "" {
		return idempotency.Response{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	publisher := v2draft.Publisher{DB: s.Drafts.DB, Objects: s.Drafts.Objects, Bucket: s.Drafts.Bucket}
	body := pending
	if len(body) == 0 {
		if s.Qualification == nil {
			return idempotency.Response{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
		}
		draft, err := s.Drafts.ReadApprovalDraft(ctx, who, tid, asset, request.DocumentID, expected)
		if err != nil {
			return idempotency.Response{}, err
		}
		// The approval reader tolerates approval replay; a new freeze requires the
		// submitted current revision exactly.
		if draft.Revision != expected {
			return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
		}
		q, err := s.Qualification.Resolve(ctx, who, tid, draft.ProjectID)
		if err != nil {
			return idempotency.Response{}, err
		}
		for _, sha := range []string{q.ModelSHA, q.RuntimeSHA, q.DeviceSHA, q.DatasetSHA} {
			if _, err := domain.ParseDigest(sha); err != nil {
				return idempotency.Response{}, v2input.ErrInvalid
			}
		}
		if q.Backend != "cpu" && q.Backend != "cuda" && q.Backend != "mps" {
			return idempotency.Response{}, v2input.ErrInvalid
		}
		loader := v2draft.RenderRecipeSource{Resolver: v2draft.ClosureResolver{Catalog: v2draft.SQLArtifactCatalog{DB: s.Drafts.DB, Bucket: s.Drafts.Bucket, ProjectID: draft.ProjectID, AssetVersionID: draft.AssetVersionID}, Objects: s.Drafts.Objects}, Learning: s.Drafts, ProjectID: draft.ProjectID}
		loaded, err := loader.Load(ctx, who, tid, draft.DocumentID, draft.Recipe, draft.Binding, draft.Image.Color.ICCSHA256, q.ModelSHA)
		if err != nil {
			return idempotency.Response{}, err
		}
		body, err = publisher.CompileSelectedFrozenRenderRecipe(ctx, who, tid, draft.ProjectID, loaded.Authored, loaded.Inputs, loaded.LayerBody, loaded.StyleBody, loaded.OverrideBodies, loaded.SourceSHA, request.AssumeProfile)
		if err != nil {
			return idempotency.Response{}, err
		}

		var completed bool
		body, completed, err = s.pinFreezePending(ctx, who, asset, scope, request, id, body)
		if err != nil {
			return idempotency.Response{}, err
		}
		if completed {
			reply, found, err := s.readFreezeReplay(ctx, who, asset, scope, request, id, &pending)
			if err != nil {
				return reply, err
			}
			if !found {
				return reply, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
			}
			return reply, nil
		}

	}
	frozen, err := v2draft.DecodeFrozenRenderRecipe(body)
	if err != nil {
		return idempotency.Response{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	binding := frozen.Scalars.Source
	// Publish only. Registration belongs to the final transaction below.
	ref, err := publisher.Publish(ctx, who, tid, binding.AssetVersionID, id, body)
	if err != nil {
		return idempotency.Response{}, err
	}
	result := RenderFreezeResult{id, request.DocumentID, request.ExpectedRevision, binding, ref}
	responseBody, err := canonical.Marshal(result)
	if err != nil || ValidateRenderFreezeResult(id, request, result) != nil {
		return idempotency.Response{}, v2input.ErrInvalid
	}
	var response idempotency.Response
	err = tenant.WithTenant(ctx, s.Drafts.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var err error
		response, err = idempotency.Use(ctx, tx, scope, request, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
			var outcome string
			err := tx.QueryRow(ctx, `SELECT photo.register_v26_render_draft_recipe_result($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, tid, binding.AssetVersionID, id, who.UserID, who.SessionID, ref.ArtifactID, ref.SHA256, binding.Source.ArtifactID, binding.Source.SHA256, string(body), request.DocumentID, expected).Scan(&outcome)
			if err != nil {
				return idempotency.Response{}, err
			}
			if outcome == "learning_expired" {
				return idempotency.Response{}, domain.NewError("SNAPSHOT_EXPIRED", 410)
			}
			if outcome != "registered" {
				if outcome != "conflict" {
					return idempotency.Response{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
				}
				return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
			}
			if err := validateFrozenLearningScope(ctx, tx, tid, asset, who, ref, frozen); err != nil {
				return idempotency.Response{}, err
			}
			return idempotency.Response{Status: 200, Body: responseBody, ResourceID: &id}, nil
		})
		return err
	})
	return response, err
}

func (s SQLRenderFreezeService) readFreezeReplay(ctx context.Context, who auth.Identity, asset uuid.UUID, scope idempotency.Scope, request RenderFreezeRequest, id uuid.UUID, pending *[]byte) (idempotency.Response, bool, error) {
	hash, err := canonical.Hash(request)
	if err != nil {
		return idempotency.Response{}, false, err
	}
	var out idempotency.Response
	found := false
	err = tenant.WithTenant(ctx, s.Drafts.DB, who, scope.Tenant, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var pid uuid.UUID
		err := tx.QueryRow(ctx, `SELECT project_id FROM photo.assets WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, scope.Tenant, asset).Scan(&pid)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		if _, err := project.Authorize(ctx, tx, scope.Tenant, pid, who.UserID, role, domain.Editor, false); err != nil {
			return err
		}
		var storedHash string
		var status *int
		var live bool
		err = tx.QueryRow(ctx, `SELECT request_hash,response_status,response_body,resource_id,expires_at>clock_timestamp() FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route=$3 AND key=$4`, scope.Tenant, scope.Principal, scope.Route, scope.Key).Scan(&storedHash, &status, &out.Body, &out.ResourceID, &live)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if storedHash != hash {
			return domain.NewError("IDEMPOTENCY_CONFLICT", 409)
		}
		if status == nil {
			if !live {
				return domain.NewError("SNAPSHOT_EXPIRED", 410)
			}
			if len(out.Body) > 0 {
				*pending, err = decodeRenderFreezePending(out.Body, id, request)
				return err
			}
			return nil
		}
		result, err := DecodeRenderFreezeResult(out.Body, id, request)
		if err != nil || *status != 200 || out.ResourceID == nil || *out.ResourceID != id {
			return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
		}
		var ready bool
		expected, _ := renderFreezeRevision(request)
		err = tx.QueryRow(ctx, `SELECT photo.v26_render_frozen_draft_scope($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, scope.Tenant, asset, id, request.DocumentID, expected, result.Recipe.ArtifactID, result.Recipe.SHA256, result.Source.AssetVersionID, result.Source.Source.ArtifactID, result.Source.Source.SHA256, who.UserID, who.SessionID).Scan(&ready)
		if err != nil {
			return err
		}
		if !ready {
			return domain.NewError("SNAPSHOT_EXPIRED", 410)
		}
		out.Body, err = canonical.Bytes(out.Body)
		if err != nil {
			return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
		}
		out.Status = *status
		out.Replayed = true
		found = true
		return nil
	})
	return out, found, err
}
