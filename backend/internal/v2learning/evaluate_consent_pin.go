package v2learning

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
)

// SQLLearningEvaluateConsentPinner serializes with consent revocation on the
// tenant row. The SQL function records grants in workflow_consent_links so a
// later revoke cancels this learning run before stale identity evidence runs.
type SQLLearningEvaluateConsentPinner struct{ DB *pgxpool.Pool }

func (s SQLLearningEvaluateConsentPinner) Pin(ctx context.Context,
	lease LearningEvaluateLease, raw []byte) error {
	if s.DB == nil || !validLearningEvaluateLease(lease) ||
		v2input.ValidateSessionConsentSnapshot(raw) != nil {
		return ErrInvalidTrainingDraft
	}
	var pinned bool
	err := s.DB.QueryRow(ctx,
		`SELECT photo.v23_pin_evaluate_identity_consents($1,$2,$3,$4,$5::jsonb)`,
		lease.TenantID, lease.RunID, lease.ExpansionID, lease.Token,
		string(raw)).Scan(&pinned)
	if err != nil {
		return err
	}
	if !pinned {
		return ErrTrainingDraftConflict
	}
	return nil
}
