package v2draft

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
)

type SQLIdentityCandidateAdopter struct {
	DB      *pgxpool.Pool
	Objects v2input.SessionAnalysisObjectReader
	Bucket  string
}

func (a SQLIdentityCandidateAdopter) AdoptIdentity(ctx context.Context,
	job IdentityAdoptionCandidate) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.DB == nil || a.Objects == nil || len(a.Bucket) < 3 || len(a.Bucket) > 63 {
		return v2input.ErrInvalid
	}
	catalog := SQLIdentityResultCatalog{DB: a.DB, Bucket: a.Bucket}
	evidence, err := PrepareIdentityAdoption(ctx, job.Scope, job.Params, catalog, catalog, a.Objects)
	if err != nil {
		return err
	}
	return (SQLIdentityAdoptionStore{DB: a.DB, Bucket: a.Bucket}).Commit(ctx, evidence, job.Attempt, job.Fence)
}
