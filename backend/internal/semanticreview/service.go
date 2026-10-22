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
	ShownRetouchTasks           []PreparedShownRetouch
	ShownBackgroundCleanupTasks []PreparedShownBackgroundCleanup
	Review                      PreparedReview
	Targets                     []LockedTarget
	Decision                    PublishedDecision
	Child                       uuid.UUID
	SourceScope                 PreparedExposureSourceScope
	Tasks                       []PreparedExposureTask
	ShownTasks                  []PreparedShownExposure
	ShownRemovalTasks           []PreparedShownRemoval
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
	if err := ctx.Err(); err != nil {
		return empty, err
	}
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
	if err := ctx.Err(); err != nil { return empty, err }
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
	shown, err := preparedApplyUsesShown(owned, prepared)
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
	if approved && shown && len(prepared.ShownRetouchTasks) > 0 {
		response, err = CommitShownRetouch(ctx, tx, tenant, intent, who.UserID, who.SessionID, prepared.Child, key, prepared.Review, prepared.Targets, owned, prepared.Decision, prepared.ShownRetouchTasks)
	} else if approved && shown && len(prepared.ShownBackgroundCleanupTasks) > 0 {
		response, err = CommitShownBackgroundCleanup(ctx, tx, tenant, intent, who.UserID, who.SessionID, prepared.Child, key, prepared.Review, prepared.Targets, owned, prepared.Decision, prepared.ShownBackgroundCleanupTasks)
	} else if approved && shown && len(prepared.ShownRemovalTasks) > 0 {
		response, err = CommitShownRemoval(ctx, tx, tenant, intent, who.UserID, who.SessionID, prepared.Child, key, prepared.Review, prepared.Targets, owned, prepared.Decision, prepared.ShownRemovalTasks)
	} else if approved && shown {
		response, err = CommitShownExposure(ctx, tx, tenant, intent, who.UserID, who.SessionID, prepared.Child, key, prepared.Review, prepared.Targets, owned, prepared.Decision, prepared.ShownTasks)
	} else if approved {
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
	if err := ctx.Err(); err != nil { return empty, err }
	return response, nil
}

// A selected shown result must never fall back to the historical rerender branch.
func preparedApplyUsesShown(request ApplyDecision, prepared PreparedApply) (bool, error) {
	approved, selected := 0, 0
	for _, decision := range request.TargetDecisions {
		if decision.Decision == "approve" {
			approved++
			if decision.Preview != nil {
				selected++
			}
		}
	}
	if approved == 0 && (prepared.Child != uuid.Nil || len(prepared.Tasks) != 0) {
		// Reject-only preparation cannot carry an ignored execution branch.
		return false, ErrInvalid
	}
	if selected == 0 {
		if len(prepared.ShownTasks) != 0 || len(prepared.ShownRemovalTasks) != 0 || len(prepared.ShownBackgroundCleanupTasks) != 0 || len(prepared.ShownRetouchTasks) != 0 {
			return false, ErrInvalid
		}
		return false, nil
	}
	// Each existing atomic commit accepts one operation family. Mixed batches need
	// a combined commit; never silently drop either family.
	if selected != approved || len(prepared.ShownTasks)+len(prepared.ShownRemovalTasks)+len(prepared.ShownBackgroundCleanupTasks)+len(prepared.ShownRetouchTasks) != approved || len(prepared.Tasks) != 0 ||
		(len(prepared.ShownTasks) > 0 && len(prepared.ShownRemovalTasks) > 0) ||
		(len(prepared.ShownBackgroundCleanupTasks) > 0 && (len(prepared.ShownTasks) > 0 || len(prepared.ShownRemovalTasks) > 0)) ||
		(len(prepared.ShownRetouchTasks) > 0 && (len(prepared.ShownTasks)+len(prepared.ShownRemovalTasks)+len(prepared.ShownBackgroundCleanupTasks) > 0)) {
		return false, ErrInvalid
	}
	return true, nil
}
