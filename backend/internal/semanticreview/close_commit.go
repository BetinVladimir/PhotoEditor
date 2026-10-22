package semanticreview

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"strconv"
	"time"
)

// CommitClose runs after publication; SQL rechecks the parent and complete
// target inventory before committing the close batch, receipt and events.
func CommitClose(ctx context.Context, tx pgx.Tx, tenant, intent, actor, session uuid.UUID, key string, prepared PreparedReview, targets []LockedTarget, request ReviewClose, published PublishedClose) (idempotency.Response, error) {
	empty := idempotency.Response{}
	if tx == nil || !idempotency.ValidKey(key) {
		return empty, ErrInvalid
	}
	raw, err := canonical.Marshal(request)
	if err != nil {
		return empty, ErrInvalid
	}
	owned, err := DecodeReviewClose(raw)
	if err != nil {
		return empty, err
	}
	proof, err := BuildCloseProof(prepared.Sources.RunID, owned, prepared.PlanRef.SHA256, strconv.FormatInt(prepared.Sources.Revision, 10), "waiting_review", targets)
	if err != nil {
		return empty, err
	}
	if published.BatchID != decisionDocumentOwner(tenant, prepared.Sources.RunID, actor, proof.RequestSHA256) || published.Ref.ArtifactID == uuid.Nil || published.Ref.SHA256 != proof.SHA256 || !bytes.Equal(published.Proof.Body, proof.Body) || published.Proof.RequestSHA256 != proof.RequestSHA256 {
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
		RunID   uuid.UUID   `json:"run_id"`
		Request ReviewClose `json:"request"`
	}{identity.RunID, owned})
	if err != nil {
		return empty, err
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return empty, err
	}
	response, err := idempotency.Use(ctx, guardReceiptAuthority(tx, tenant, intent, actor, session, identity), idempotency.Scope{Tenant: tenant, Principal: actor, PrincipalType: "user", Route: CloseIdempotencyRoute, Key: key}, json.RawMessage(fingerprint), time.Now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
		var body json.RawMessage
		err := tx.QueryRow(ctx, `SELECT photo.commit_semantic_review_close($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb)`, tenant, intent, actor, session, key, prepared.Sources.Revision, string(fingerprint), published.Ref.ArtifactID, string(proof.Body), json.RawMessage(documents)).Scan(&body)
		if err != nil {
			return empty, err
		}
		result, err := DecodeCloseResult(body)
		if err != nil || result.WorkflowID != identity.WorkflowID || result.RunID != identity.RunID || result.Revision != strconv.FormatInt(prepared.Sources.Revision+1, 10) {
			return empty, ErrInvalid
		}
		batch := published.BatchID
		return idempotency.Response{Status: 200, Body: body, ResourceID: &batch}, nil
    })
    if err != nil { return empty, err }
    if response.Replayed {
        return verifyCloseCommitReplay(ctx, response, identity.RunID, identity.WorkflowID, published.BatchID,
            func(ctx context.Context) (ReplayResult, error) {
                return ReadCloseReplay(ctx, tx, tenant, intent, actor, session, key, proof.RequestSHA256)
            })
    }
    return response, nil
}

// Idempotency.Use may wait for a concurrent close and bypass the mutation
// callback. Repeat authenticated replay validation after that wait as well.
func verifyCloseCommitReplay(ctx context.Context, response idempotency.Response, run, workflow, batch uuid.UUID,
    load func(context.Context) (ReplayResult, error)) (idempotency.Response, error) {
    empty := idempotency.Response{}
    if err := ctx.Err(); err != nil { return empty, err }
    if load == nil || !response.Replayed || response.Status != 200 || response.ResourceID == nil || *response.ResourceID != batch {
        return empty, ErrInvalid
    }
    replay, err := load(ctx)
    if err != nil { return empty, err }
    if err := ctx.Err(); err != nil { return empty, err }
    if replay.Outcome != "replayed" { return empty, ErrConflict }
    result, err := DecodeCloseResult(replay.Body)
    if err != nil || result.RunID != run || result.WorkflowID != workflow { return empty, ErrInvalid }
    body, err := canonical.Bytes(replay.Body)
    if err != nil || !bytes.Equal(body, response.Body) { return empty, ErrInvalid }
    response.Body = append(json.RawMessage(nil), body...)
    return response, nil
}
