package semanticreview

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
)

const ApplyIdempotencyRoute = "SemanticIntent.Apply"

type ReplayResult struct {
	Outcome string
	Body    json.RawMessage
}

// ReadApplyReplay precedes storage reads. Current authorization is checked by
// SQL; a completed response belongs to this intent's immutable review batch.
// Pending/conflict must never be interpreted as permission to run admission.
func ReadApplyReplay(ctx context.Context, tx pgx.Tx, tenant, intent, actor, session uuid.UUID, key, requestSHA string) (ReplayResult, error) {
	if tx == nil || tenant == uuid.Nil || intent == uuid.Nil || actor == uuid.Nil || session == uuid.Nil || !idempotency.ValidKey(key) || !shaPattern.MatchString(requestSHA) {
		return ReplayResult{}, ErrInvalid
	}
	var result ReplayResult
	if err := tx.QueryRow(ctx, `SELECT outcome,response_body FROM photo.semantic_apply_replay($1,$2,$3,$4,$5,$6)`, tenant, intent, actor, session, key, requestSHA).Scan(&result.Outcome, &result.Body); err != nil {
		return ReplayResult{}, err
	}
	switch result.Outcome {
	case "absent", "pending", "conflict":
		if len(result.Body) != 0 {
			return ReplayResult{}, ErrInvalid
		}
	case "replayed":
		if _, err := DecodeBatchResult(result.Body); err != nil {
			return ReplayResult{}, ErrInvalid
		}
		body, err := canonical.Bytes(result.Body)
		if err != nil {
			return ReplayResult{}, ErrInvalid
		}
		result.Body = body
	default:
		return ReplayResult{}, ErrInvalid
	}
	result.Body = append(json.RawMessage(nil), result.Body...)
	return result, nil
}

// HashApplyRequest binds the idempotency fingerprint to the parent run, not
// just an identical request body from another intent. Obtain run ID from the
// authenticated intent metadata before any expiring object reads.
func HashApplyRequest(parentRun uuid.UUID, request ApplyDecision) (string, error) {
	if parentRun == uuid.Nil {
		return "", ErrInvalid
	}
	raw, err := canonical.Marshal(request)
	if err != nil {
		return "", ErrInvalid
	}
	verified, err := DecodeApplyDecision(raw)
	if err != nil {
		return "", err
	}
	return canonical.Hash(struct {
		RunID   uuid.UUID     `json:"run_id"`
		Request ApplyDecision `json:"request"`
	}{parentRun, verified})
}

type ReviewIdentity struct {
	RunID      uuid.UUID
	WorkflowID uuid.UUID
	ProjectID  uuid.UUID
}

// LoadReviewIdentity requires current editor authorization, but no plan object
// read. Closed reviews can therefore replay their previously committed receipts.
func LoadReviewIdentity(ctx context.Context, tx pgx.Tx, tenant, intent, actor, session uuid.UUID) (ReviewIdentity, error) {
	if tx == nil || tenant == uuid.Nil || intent == uuid.Nil || actor == uuid.Nil || session == uuid.Nil {
		return ReviewIdentity{}, ErrInvalid
	}
	var identity ReviewIdentity
	if err := tx.QueryRow(ctx, `SELECT run_id,workflow_id,project_id FROM photo.semantic_review_identity($1,$2,$3,$4)`, tenant, intent, actor, session).Scan(&identity.RunID, &identity.WorkflowID, &identity.ProjectID); err != nil {
		return ReviewIdentity{}, err
	}
	if identity.RunID == uuid.Nil || identity.WorkflowID == uuid.Nil || identity.ProjectID == uuid.Nil {
		return ReviewIdentity{}, ErrInvalid
	}
	return identity, nil
}

// ReadRequestReplay derives the scoped fingerprint from authenticated persisted
// identity. Caller receives no permission to admit on pending/conflict outcomes.
func ReadRequestReplay(ctx context.Context, tx pgx.Tx, tenant, intent, actor, session uuid.UUID, key string, request ApplyDecision) (ReplayResult, error) {
	if !idempotency.ValidKey(key) {
		return ReplayResult{}, ErrInvalid
	}
	// Own and validate the request before database callbacks; the original
	// TargetDecisions slice may otherwise change while identity is loaded.
	raw, err := canonical.Marshal(request)
	if err != nil {
		return ReplayResult{}, ErrInvalid
	}
	owned, err := DecodeApplyDecision(raw)
	if err != nil {
		return ReplayResult{}, err
	}
	identity, err := LoadReviewIdentity(ctx, tx, tenant, intent, actor, session)
	if err != nil {
		return ReplayResult{}, err
	}
	sha, err := HashApplyRequest(identity.RunID, owned)
	if err != nil {
		return ReplayResult{}, err
	}
	replay, err := ReadApplyReplay(ctx, tx, tenant, intent, actor, session, key, sha)
	if err != nil {
		return ReplayResult{}, err
	}
	if replay.Outcome == "replayed" {
		batch, err := DecodeBatchResult(replay.Body)
		if err != nil || batch.WorkflowID != identity.WorkflowID {
			return ReplayResult{}, ErrConflict
		}
	}
	return replay, nil
}
