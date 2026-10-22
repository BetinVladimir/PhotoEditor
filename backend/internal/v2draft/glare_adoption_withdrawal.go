package v2draft

import (
	"context"

	"photoeditor/backend/internal/v2input"
)

type RestorationAdoptionMaintenance interface { ProcessOne(context.Context) (bool, error) }

// SQL independently classifies persisted revocation. Infrastructure/read errors
// from the adopter never become a permanent authorization-loss witness.
type SQLGlareAdoptionWithdrawal struct { DB RestorationAdoptionQuery }

func (s SQLGlareAdoptionWithdrawal) ProcessOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil { return false, err }
	if s.DB == nil { return false, v2input.ErrInvalid }
	for _, query := range []string{"SELECT photo.withdraw_completed_glare_consent()", "SELECT photo.release_next_glare_withdrawn_quota()"} {
		var changed bool
		if err := s.DB.QueryRow(ctx, query).Scan(&changed); err != nil { return false, err }
		if err := ctx.Err(); err != nil { return false, err }
		if changed { return true, nil }
	}
	return false, nil
}
