package v2draft

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

type IdentityAdoptionTransactions interface {
	Begin(context.Context) (pgx.Tx, error)
}

type SQLIdentityAdoptionStore struct {
	DB     IdentityAdoptionTransactions
	Bucket string
}

// Commit publishes a physically verified identity result only while the
// source, consent, qualification and exact object-version locks are held.
func (s SQLIdentityAdoptionStore) Commit(ctx context.Context,
	evidence PreparedIdentityAdoption, attempt int, fence int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	scope := evidence.Scope
	if s.DB == nil || !evidence.verified || scope.Tenant == uuid.Nil ||
		scope.Task == uuid.Nil || scope.Result == uuid.Nil ||
		attempt <= 0 || fence <= 0 || len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return v2input.ErrInvalid
	}
	inventory, digest, err := BuildIdentityAdoptionInventory(evidence)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(bounded)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(bounded))
	if err = CheckIdentityLiveSourcesInTx(bounded, tx, evidence); err != nil {
		return err
	}
	if err = CheckIdentityLiveConsentsInTx(bounded, tx, evidence); err != nil {
		return err
	}
	if _, err = CheckIdentityLiveQualificationInTx(bounded, tx, evidence); err != nil {
		return err
	}
	if err = CheckIdentityObjectPinsInTx(bounded, tx, evidence, s.Bucket); err != nil {
		return err
	}
	var adopted bool
	err = tx.QueryRow(bounded, `SELECT photo.adopt_v23_identity_verified($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10,$11,$12,$13)`,
		scope.Tenant, scope.Task, attempt, fence, scope.Result, string(inventory), digest,
		evidence.Calibration.Ref.ArtifactID, evidence.Calibration.Ref.SHA256,
		evidence.Result.Mask.ArtifactID, evidence.Result.Mask.SHA256,
		evidence.Result.Pixels.ArtifactID, evidence.Result.Pixels.SHA256).Scan(&adopted)
	if err != nil {
		return err
	}
	if !adopted {
		return v2input.ErrExpired
	}
	return tx.Commit(bounded)
}
