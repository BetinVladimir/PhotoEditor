package semanticreview

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"time"
)

// CommitApprovedExposure composes the complete mutation in the caller transaction.
// Publication/physical preflight must finish before entry. The trusted transaction
// role must execute private SQL components; this function grants no DB authority.
// Caller must roll back every error and commit only the returned successful receipt.
// A matching completed receipt replays without requiring current source artifacts.
func CommitApprovedExposure(ctx context.Context, tx pgx.Tx, tenant, intent, actor, session, child uuid.UUID,
	key string, prepared PreparedReview, targets []LockedTarget, request ApplyDecision,
	decision PublishedDecision, scope PreparedExposureSourceScope, published []PreparedExposureTask) (idempotency.Response, error) {
	empty := idempotency.Response{}
	if tx == nil || !idempotency.ValidKey(key) {
		return empty, ErrInvalid
	}
	raw, err := canonical.Marshal(request)
	if err != nil {
		return empty, ErrInvalid
	}
	owned, err := DecodeApplyDecision(raw)
	if err != nil {
		return empty, err
	}
	identity, err := LoadReviewIdentity(ctx, tx, tenant, intent, actor, session)
	if err != nil {
		return empty, err
	}
	fingerprint, err := canonical.Marshal(struct {
		RunID   uuid.UUID     `json:"run_id"`
		Request ApplyDecision `json:"request"`
	}{identity.RunID, owned})
	if err != nil {
		return empty, ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return empty, err
	}
	response, err := idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tenant, Principal: actor, PrincipalType: "user", Route: ApplyIdempotencyRoute, Key: key}, json.RawMessage(fingerprint), time.Now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
		if prepared.Sources.RunID != identity.RunID {
			return empty, ErrConflict
		}
		var locked bool
		if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_apply_resources($1,$2,$3,$4)", tenant, intent, actor, session).Scan(&locked); err != nil {
			return empty, err
		}
		if !locked {
			return empty, ErrConflict
		}
		inputs, err := CreateSourceVerifiedExposureDraftInTransaction(ctx, tx, tenant, identity.ProjectID, intent, actor, session, child, prepared, targets, owned, decision, scope, published)
		if err != nil {
			return empty, err
		}
		var body json.RawMessage
		err = tx.QueryRow(ctx, "SELECT photo.finish_semantic_apply_review($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)", tenant, intent, actor, session, key, prepared.Sources.Revision, string(fingerprint), decision.Ref.ArtifactID, string(inputs.Review.Decision.Body), child, inputs.Review.Params, inputs.Nodes, inputs.Transport, inputs.Inventory).Scan(&body)
		if err != nil {
			return empty, err
		}
		result, err := DecodeBatchResult(body)
		if err != nil || result.WorkflowID != identity.WorkflowID || result.BatchID != decision.BatchID || result.Child == nil || result.Child.RunID != child {
			return empty, ErrInvalid
		}
		batch := result.BatchID
		return idempotency.Response{Status: 200, Body: body, ResourceID: &batch}, nil
	})
	if err != nil {
		return empty, err
	}
	result, err := DecodeBatchResult(response.Body)
	if err != nil || response.Status != 200 || result.WorkflowID != identity.WorkflowID || result.Child == nil || response.ResourceID == nil || *response.ResourceID != result.BatchID {
		return empty, ErrInvalid
	}
	return response, nil
}
