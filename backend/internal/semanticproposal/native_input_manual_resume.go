package semanticproposal

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Internal coordinator for an existing original registered manual preparation.
// Loads saved declarations/Params/report/costs/producer results itself, then
// resumes with complete current admission. Missing state is never recreated.
func (s SQLTransferSourceReader) restoreAndResumeNativeInputManualPreparation(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, producers nativeInputRestoredFaceProducers, color TransferColorProfile) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	plan, err := s.restoreNativeInputManualDraft(ctx, scheduler, who, producers, color)
	if err != nil {
		return zero, err
	}
	return s.resumeNativeInputManualPreparation(ctx, scheduler, who, plan, color)
}
