package semanticreview

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
)

var ErrUnavailable = errors.New("semantic review service unavailable")

// PreparedApply contains published documents and opaque verified source authority.
// Prepare runs outside DB transactions, using scoped/versioned reads and publishers.
type PreparedApply struct {
	Review      PreparedReview
	Targets     []LockedTarget
	Decision    PublishedDecision
	Child       uuid.UUID
	SourceScope PreparedExposureSourceScope
	Tasks       []PreparedExposureTask
}

type ApplyPreparation func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, ApplyDecision) (PreparedApply, error)

// ApplyService owns transaction boundaries; admission pool must be explicitly
// provisioned to execute private approved SQL components. It grants no privileges.
type ApplyService struct {
	API       *pgxpool.Pool
	Admission *pgxpool.Pool
	Prepare   ApplyPreparation
}

func (s *ApplyService) Apply(ctx context.Context, who auth.Identity, tenant, intent uuid.UUID, key string, request ApplyDecision) (idempotency.Response, error) {
	empty := idempotency.Response{}
	if tenant == uuid.Nil || intent == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !idempotency.ValidKey(key) {
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
	if s == nil || s.API == nil {
		return empty, ErrUnavailable
	}
	replayTX, err := s.API.Begin(ctx)
	if err != nil {
		return empty, err
	}
	replay, err := ReadRequestReplay(ctx, replayTX, tenant, intent, who.UserID, who.SessionID, key, owned)
	rollbackErr := replayTX.Rollback(context.Background())
	if err != nil {
		return empty, err
	}
	if rollbackErr != nil {
		return empty, rollbackErr
	}
	switch replay.Outcome {
	case "replayed":
		result, err := DecodeBatchResult(replay.Body)
		if err != nil {
			return empty, err
		}
		batch := result.BatchID
		return idempotency.Response{Status: 200, Body: replay.Body, ResourceID: &batch, Replayed: true}, nil
	case "pending", "conflict":
		return empty, ErrConflict
	case "absent":
	default:
		return empty, ErrInvalid
	}
	if s.Prepare == nil {
		return empty, ErrUnavailable
	}
	approved := false
	for _, decision := range owned.TargetDecisions {
		if decision.Decision == "approve" {
			approved = true
		}
	}
	if approved && s.Admission == nil {
		return empty, ErrUnavailable
	}
	// An independently owned request prevents a preparer from changing the final
	// receipt fingerprint. No object/model IO is permitted after this call returns.
	prepareRequest, err := DecodeApplyDecision(raw)
	if err != nil {
		return empty, err
	}
	prepared, err := s.Prepare(ctx, who, tenant, intent, prepareRequest)
	if err != nil {
		return empty, err
	}
	pool := s.API
	if approved {
		pool = s.Admission
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(context.Background())
	var response idempotency.Response
	if approved {
		response, err = CommitApprovedExposure(ctx, tx, tenant, intent, who.UserID, who.SessionID, prepared.Child, key, prepared.Review, prepared.Targets, owned, prepared.Decision, prepared.SourceScope, prepared.Tasks)
	} else {
		response, err = CommitRejections(ctx, tx, tenant, intent, who.UserID, who.SessionID, key, prepared.Review, prepared.Targets, owned, prepared.Decision)
	}
	if err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return response, nil
}
