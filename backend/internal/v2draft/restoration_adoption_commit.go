package v2draft

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type SQLRestorationAdoptionStore struct {
	DB RestorationAdoptionTransactions
}

type RestorationAdoptionTransactions interface {
	Begin(context.Context) (pgx.Tx, error)
}

// Commit atomically pins the physically checked object versions to the
// canonical worker result. It does not decide the human review or Apply.
func (s SQLRestorationAdoptionStore) Commit(ctx context.Context,
	evidence RestorationVerificationEvidence) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	scope := evidence.Scope
	if s.DB == nil || !evidence.verified || scope.Tenant == uuid.Nil || scope.Task == uuid.Nil ||
		scope.Result == uuid.Nil || scope.Root == uuid.Nil ||
		!isDocumentRef(v2input.Ref{ArtifactID: scope.Root, SHA256: scope.RootSHA}) ||
		evidence.Candidate.Result.ArtifactID != scope.Root ||
		evidence.Candidate.Result.SHA256 != scope.RootSHA ||
		len(evidence.Bucket) < 3 || len(evidence.Bucket) > 63 ||
		(len(evidence.Objects) != 10 && len(evidence.Objects) != 12) {
		return v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(evidence)
	if err != nil || len(raw) < 2 || len(raw) > 65536 {
		return v2input.ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(bounded)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(bounded))
	if _, err = tx.Exec(bounded, "SELECT set_config('app.tenant_id',$1,true)", scope.Tenant.String()); err != nil {
		return err
	}
	var role string
	if err = tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var accepted bool
	if err = tx.QueryRow(bounded, "SELECT photo.adopt_v28_restoration_verified($1,$2,$3,$4)",
		scope.Tenant, scope.Task, scope.Result, string(raw)).Scan(&accepted); err != nil {
		return err
	}
	if !accepted {
		return v2input.ErrExpired
	}
	var reviewID uuid.UUID
	if err = tx.QueryRow(bounded, "SELECT photo.create_v28_restoration_review($1,$2,$3)",
		scope.Tenant, scope.Task, scope.Result).Scan(&reviewID); err != nil {
		return err
	}
	if reviewID == uuid.Nil {
		return v2input.ErrExpired
	}
	return tx.Commit(bounded)
}
