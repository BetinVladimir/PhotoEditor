package semanticreview

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
)

// createApprovedExposureDraftInTransaction composes private draft creation and
// input registration/seal from independently reverified publication evidence.
// Caller must already hold the idempotency receipt and MUST roll back on any
// error. It must finish source-byte authority, measured resource costs, quota,
// activation, parent revision, receipt and events in this same transaction.
// This does not dispatch or commit. SQL functions remain private components of
// the future final admission; no new runtime-role permission is implied.
func createApprovedExposureDraftInTransaction(ctx context.Context, tx pgx.Tx,
	tenant, project, intent, actor, session, child uuid.UUID, prepared PreparedReview,
	targets []LockedTarget, request ApplyDecision, decision PublishedDecision,
	inputs []ExposureTaskInputs, published []PreparedExposureTask) (ExposureCommitInputs, error) {
	empty := ExposureCommitInputs{}
	if tx == nil || tenant == uuid.Nil || project == uuid.Nil || intent == uuid.Nil || actor == uuid.Nil || session == uuid.Nil || child == uuid.Nil {
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
	verified, err := PrepareApprovedExposureCommitInputs(ctx, tenant, actor, child, prepared, targets, owned, decision, inputs, published)
	if err != nil {
		return empty, err
	}
	fingerprint := struct {
		RunID   uuid.UUID     `json:"run_id"`
		Request ApplyDecision `json:"request"`
	}{prepared.Sources.RunID, owned}
	fingerprintRaw, err := canonical.Marshal(fingerprint)
	if err != nil {
		return empty, ErrInvalid
	}
	fingerprintSHA, err := canonical.Hash(fingerprint)
	if err != nil || fingerprintSHA != verified.Review.Decision.RequestSHA256 {
		return empty, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	var batch uuid.UUID
	if err := tx.QueryRow(ctx, "SELECT photo.create_semantic_apply_draft_shell($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)",
		tenant, intent, actor, session, prepared.Sources.Revision, string(fingerprintRaw), decision.Ref.ArtifactID, string(verified.Review.Decision.Body), json.RawMessage(verified.Review.Documents), child, json.RawMessage(verified.Review.Params), verified.Nodes).Scan(&batch); err != nil {
		return empty, err
	}
	if batch != decision.BatchID {
		return empty, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return registerVerifiedExposureInputs(ctx, tx, tenant, project, prepared.Sources.RunID, child, verified)
}
