package maintenance

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
)

// TenantDeletion is a fenced, bounded unit of a physical tenant deletion job.
// Database rows are retained until object storage reports an empty tenant prefix.
type TenantDeletion struct {
	JobID, Token, TenantID uuid.UUID
}

type TenantDeletionRepository interface {
	Claim(context.Context) (*TenantDeletion, error)
	Checkpoint(context.Context, TenantDeletion) (bool, error)
	Finish(context.Context, TenantDeletion) (bool, error)
}

type TenantDeletionObjects interface {
	// PurgeTenantBatch removes at most limit exact object versions and reports
	// whether another bounded pass is required.
	PurgeTenantBatch(context.Context, uuid.UUID, int) (bool, error)
}

type TenantCleaner struct {
	Repository TenantDeletionRepository
	Objects    TenantDeletionObjects
	Batch      int
}

type SQLTenantDeletion struct{ DB artifactjson.RowQuerier }

func (s SQLTenantDeletion) Claim(ctx context.Context) (*TenantDeletion, error) {
	if s.DB == nil {
		return nil, errors.New("tenant deletion database required")
	}
	t := new(TenantDeletion)
	err := s.DB.QueryRow(ctx, "SELECT job_id,cleanup_token,tenant_id FROM photo.claim_tenant_deletion()").Scan(&t.JobID, &t.Token, &t.TenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}
func (s SQLTenantDeletion) Checkpoint(ctx context.Context, t TenantDeletion) (bool, error) {
	return s.transition(ctx, "SELECT photo.checkpoint_tenant_deletion($1,$2)", t)
}
func (s SQLTenantDeletion) Finish(ctx context.Context, t TenantDeletion) (bool, error) {
	return s.transition(ctx, "SELECT photo.finish_tenant_deletion($1,$2)", t)
}
func (s SQLTenantDeletion) transition(ctx context.Context, q string, t TenantDeletion) (bool, error) {
	if s.DB == nil || t.JobID == uuid.Nil || t.Token == uuid.Nil || t.TenantID == uuid.Nil {
		return false, errors.New("invalid tenant deletion transition")
	}
	var ok bool
	err := s.DB.QueryRow(ctx, q, t.JobID, t.Token).Scan(&ok)
	return ok, err
}

func (c TenantCleaner) CleanOne(ctx context.Context) (bool, error) {
	if c.Repository == nil || c.Objects == nil || c.Batch < 1 || c.Batch > 256 {
		return false, errors.New("invalid tenant cleanup configuration")
	}
	claim, cancel := context.WithTimeout(ctx, 5*time.Second)
	ticket, err := c.Repository.Claim(claim)
	cancel()
	if err != nil || ticket == nil {
		return false, err
	}
	if ticket.JobID == uuid.Nil || ticket.Token == uuid.Nil || ticket.TenantID == uuid.Nil {
		return false, errors.New("invalid tenant cleanup ticket")
	}
	purge, cancel := context.WithTimeout(ctx, 40*time.Second)
	more, err := c.Objects.PurgeTenantBatch(purge, ticket.TenantID, c.Batch)
	cancel()
	if err != nil {
		return false, err
	}
	commit, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if more {
		return c.Repository.Checkpoint(commit, *ticket)
	}
	return c.Repository.Finish(commit, *ticket)
}
