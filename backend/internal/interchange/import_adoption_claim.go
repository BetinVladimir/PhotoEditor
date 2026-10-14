package interchange

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
)

// ImportAdoptionClaim only leases discovery. It grants no permission to read
// output objects or commit adoption; both require current SQL authority.
type ImportAdoptionClaim struct {
	Tenant, Workflow, Run, Task, Result uuid.UUID
	Attempt                             int32
	Fence                               int64
	Token                               uuid.UUID
}

func (c ImportAdoptionClaim) validate() error {
	if c.Tenant == uuid.Nil || c.Workflow == uuid.Nil || c.Run == uuid.Nil ||
		c.Task == uuid.Nil || c.Result == uuid.Nil || c.Token == uuid.Nil ||
		c.Attempt < 1 || c.Fence < 1 ||
		c.Task != uuid.NewSHA1(c.Workflow, []byte("v2.interchange.import:0")) {
		return v2input.ErrInvalid
	}
	return nil
}

type SQLImportAdoptionClaimStore struct{ DB *pgxpool.Pool }

func (s SQLImportAdoptionClaimStore) NextImportAdoption(ctx context.Context) (*ImportAdoptionClaim, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil {
		return nil, v2input.ErrInvalid
	}
	var claim ImportAdoptionClaim
	err := s.DB.QueryRow(ctx, `SELECT tenant_id,workflow_id,run_id,task_id,result_id,attempt,fencing_token,claim_token FROM photo.v30_claim_next_import_adoption()`).
		Scan(&claim.Tenant, &claim.Workflow, &claim.Run, &claim.Task, &claim.Result,
			&claim.Attempt, &claim.Fence, &claim.Token)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := claim.validate(); err != nil {
		return nil, err
	}
	return &claim, ctx.Err()
}

func (s SQLImportAdoptionClaimStore) CurrentImportAdoptionClaim(ctx context.Context, claim ImportAdoptionClaim) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.DB == nil || claim.validate() != nil {
		return false, v2input.ErrInvalid
	}
	var current bool
	err := s.DB.QueryRow(ctx, `SELECT photo.v30_import_adoption_claim_current($1,$2,$3,$4,$5,$6,$7,$8)`,
		claim.Tenant, claim.Workflow, claim.Run, claim.Task, claim.Result, claim.Attempt,
		claim.Fence, claim.Token).Scan(&current)
	if err != nil {
		return false, err
	}
	return current, ctx.Err()
}

func (s SQLImportAdoptionClaimStore) RenewImportAdoptionClaim(ctx context.Context, claim ImportAdoptionClaim) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.DB == nil || claim.validate() != nil {
		return false, v2input.ErrInvalid
	}
	var renewed bool
	err := s.DB.QueryRow(ctx, `SELECT photo.v30_renew_import_adoption_claim($1,$2,$3,$4,$5,$6,$7,$8)`,
		claim.Tenant, claim.Workflow, claim.Run, claim.Task, claim.Result, claim.Attempt,
		claim.Fence, claim.Token).Scan(&renewed)
	if err != nil {
		return false, err
	}
	return renewed, ctx.Err()
}
