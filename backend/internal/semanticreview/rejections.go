package semanticreview

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"time"
)

// CommitRejections runs in a fresh caller transaction after publication. The
// receipt, every selected target, parent revision and events commit together.
// It is solely the reject-only branch; approvals require child Apply admission.
func CommitRejections(ctx context.Context, tx pgx.Tx, tenant, intent, actor, session uuid.UUID, key string, prepared PreparedReview, targets []LockedTarget, request ApplyDecision, published PublishedDecision) (idempotency.Response, error) {
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
	proof, err := BuildDecisionProof(prepared, targets, owned)
	if err != nil {
		return empty, err
	}
	if len(proof.Approved) != 0 || published.BatchID != decisionDocumentOwner(tenant, prepared.Sources.RunID, actor, proof.RequestSHA256) || published.Ref.ArtifactID == uuid.Nil || published.Ref.SHA256 != proof.SHA256 || !bytes.Equal(published.Proof.Body, proof.Body) || published.Proof.RequestSHA256 != proof.RequestSHA256 {
		return empty, ErrInvalid
	}
	documents, err := EncodeInitializationDocuments(prepared)
	if err != nil {
		return empty, err
	}
	identity, err := LoadReviewIdentity(ctx, tx, tenant, intent, actor, session)
	if err != nil {
		return empty, err
	}
	if identity.RunID != prepared.Sources.RunID {
		return empty, ErrConflict
	}
	fingerprint, err := canonical.Marshal(struct {
		RunID   uuid.UUID     `json:"run_id"`
		Request ApplyDecision `json:"request"`
	}{identity.RunID, owned})
	if err != nil {
		return empty, err
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return empty, err
	}
	return idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tenant, Principal: actor, PrincipalType: "user", Route: ApplyIdempotencyRoute, Key: key}, json.RawMessage(fingerprint), time.Now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
		var body json.RawMessage
		err := tx.QueryRow(ctx, `SELECT photo.commit_semantic_rejections($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb)`, tenant, intent, actor, session, key, prepared.Sources.Revision, string(fingerprint), published.Ref.ArtifactID, string(proof.Body), json.RawMessage(documents)).Scan(&body)
		if err != nil {
			return empty, err
		}
		result, err := DecodeBatchResult(body)
		if err != nil || result.WorkflowID != identity.WorkflowID || result.BatchID != published.BatchID || result.Child != nil {
			return empty, ErrInvalid
		}
		batch := published.BatchID
		return idempotency.Response{Status: 200, Body: body, ResourceID: &batch}, nil
	})
}
