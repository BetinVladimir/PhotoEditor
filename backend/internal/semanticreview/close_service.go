package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
)

type PreparedClose struct {
	Review   PreparedReview
	Targets  []LockedTarget
	Decision PublishedClose
}
type ClosePreparation func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, ReviewClose) (PreparedClose, error)
type CloseService struct {
	API     *pgxpool.Pool
	Prepare ClosePreparation
}

// Close checks replay before object IO; fresh preparation owns publication.
// The final transaction repeats all source and target admission checks.
func (s *CloseService) Close(ctx context.Context, who auth.Identity, tenant, intent uuid.UUID, key string, request ReviewClose) (idempotency.Response, error) {
	empty := idempotency.Response{}
	if tenant == uuid.Nil || intent == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !idempotency.ValidKey(key) {
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
	if s == nil || s.API == nil {
		return empty, ErrUnavailable
	}
	tx, err := s.API.Begin(ctx)
	if err != nil {
		return empty, err
	}
	identity, err := LoadReviewIdentity(ctx, tx, tenant, intent, who.UserID, who.SessionID)
	var replay ReplayResult
	if err == nil {
		var sha string
		sha, err = HashCloseRequest(identity.RunID, owned)
		if err == nil {
			replay, err = ReadCloseReplay(ctx, tx, tenant, intent, who.UserID, who.SessionID, key, sha)
		}
	}
	rollbackErr := tx.Rollback(context.Background())
	if err != nil {
		return empty, err
	}
	if rollbackErr != nil {
		return empty, rollbackErr
	}
	switch replay.Outcome {
	case "replayed":
		if _, err := DecodeCloseResult(replay.Body); err != nil {
			return empty, err
		}
		// Resource is the immutable close batch, derived from the same fingerprint.
		sha, _ := HashCloseRequest(identity.RunID, owned)
		batch := decisionDocumentOwner(tenant, identity.RunID, who.UserID, sha)
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
	prepared, err := s.Prepare(ctx, who, tenant, intent, owned)
	if err != nil {
		return empty, err
	}
	tx, err = s.API.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(context.Background())
	response, err := CommitClose(ctx, tx, tenant, intent, who.UserID, who.SessionID, key, prepared.Review, prepared.Targets, owned, prepared.Decision)
	if err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return response, nil
}
