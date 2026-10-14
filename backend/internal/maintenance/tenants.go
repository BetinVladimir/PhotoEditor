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
	SettleArchiveGenerations(context.Context, TenantDeletion) (string, error)
	RenewLease(context.Context, TenantDeletion) (bool, error)
	Finish(context.Context, TenantDeletion) (bool, error)
}

type TenantDeletionObjects interface {
	// PurgeTenantBatch removes at most limit exact object versions and reports
	// whether another bounded pass is required.
	PurgeTenantBatch(context.Context, uuid.UUID, int) (bool, error)
}

var ErrTenantDeletionLeaseLost = errors.New("tenant deletion lease lost")

type TenantCleaner struct {
	Repository     TenantDeletionRepository
	Objects        TenantDeletionObjects
	Batch          int
	History        TenantDeletionHistory
	RequireHistory bool
}

// Two independently bounded physical scans surround SQL settlement. Renewals
// before each scan keep the 60-second DB lease fenced throughout.
func (TenantCleaner) CleanupCycleTimeout() time.Duration { return 120 * time.Second }

type SQLTenantDeletion struct{ DB artifactjson.RowQuerier }

func (c TenantCleaner) renewLease(ctx context.Context, ticket TenantDeletion) error {
	renew, stop := context.WithTimeout(ctx, 5*time.Second)
	renewed, err := c.Repository.RenewLease(renew, ticket)
	renewErr := renew.Err()
	stop()
	if err != nil {
		return err
	}
	if renewErr != nil {
		return renewErr
	}
	if !renewed {
		return ErrTenantDeletionLeaseLost
	}
	return nil
}

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
func (s SQLTenantDeletion) SettleArchiveGenerations(ctx context.Context, t TenantDeletion) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.DB == nil || t.JobID == uuid.Nil || t.Token == uuid.Nil || t.TenantID == uuid.Nil {
		return "", errors.New("invalid tenant deletion transition")
	}
	var state string
	err := s.DB.QueryRow(ctx, "SELECT photo.settle_v30_archive_generations_after_tenant_purge($1,$2,$3)", t.JobID, t.Token, t.TenantID).Scan(&state)
	if err != nil {
		return "", err
	}
	if state != "blocked" && state != "more" && state != "complete" {
		return "", errors.New("invalid archive generation settlement result")
	}
	return state, nil
}
func (s SQLTenantDeletion) RenewLease(ctx context.Context, t TenantDeletion) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.DB == nil || t.JobID == uuid.Nil || t.Token == uuid.Nil || t.TenantID == uuid.Nil {
		return false, errors.New("invalid tenant deletion transition")
	}
	var renewed bool
	err := s.DB.QueryRow(ctx, "SELECT photo.renew_tenant_deletion_lease($1,$2,$3)", t.JobID, t.Token, t.TenantID).Scan(&renewed)
	return renewed, err
}
func (s SQLTenantDeletion) transition(ctx context.Context, q string, t TenantDeletion) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.DB == nil || t.JobID == uuid.Nil || t.Token == uuid.Nil || t.TenantID == uuid.Nil {
		return false, errors.New("invalid tenant deletion transition")
	}
	var ok bool
	err := s.DB.QueryRow(ctx, q, t.JobID, t.Token).Scan(&ok)
	return ok, err
}

func (c TenantCleaner) CleanOne(ctx context.Context) (bool, error) {
	if c.Repository == nil || c.Objects == nil || c.Batch < 1 || c.Batch > 256 || c.RequireHistory && c.History == nil {
		return false, errors.New("invalid tenant cleanup configuration")
	}
	claim, cancel := context.WithTimeout(ctx, 5*time.Second)
	ticket, err := c.Repository.Claim(claim)
	claimErr := claim.Err()
	cancel()
	if err != nil {
		return false, err
	}
	if claimErr != nil {
		return false, claimErr
	}
	if ticket == nil {
		return false, nil
	}
	if ticket.JobID == uuid.Nil || ticket.Token == uuid.Nil || ticket.TenantID == uuid.Nil {
		return false, errors.New("invalid tenant cleanup ticket")
	}
	if c.History != nil {
		record, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = c.History.RecordRequested(record, *ticket)
		recordErr := record.Err()
		cancel()
		if err != nil {
			return false, err
		}
		if recordErr != nil {
			return false, recordErr
		}
	}
	if err := c.renewLease(ctx, *ticket); err != nil {
		return false, err
	}
	purge, cancel := context.WithTimeout(ctx, 40*time.Second)
	more, err := c.Objects.PurgeTenantBatch(purge, ticket.TenantID, c.Batch)
	purgeErr := purge.Err()
	cancel()
	if err != nil {
		return false, err
	}
	// A storage adapter may ignore cancellation and report an empty prefix.
	// Never make the irreversible metadata transition on that observation.
	if purgeErr != nil {
		return false, purgeErr
	}
	if more {
		checkpoint, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := checkpoint.Err(); err != nil {
			return false, err
		}
		accepted, err := c.Repository.Checkpoint(checkpoint, *ticket)
		if err != nil {
			return false, err
		}
		if err := checkpoint.Err(); err != nil {
			return false, err
		}
		if !accepted {
			return false, ErrTenantDeletionLeaseLost
		}
		return accepted, nil
	}
	settlement, stopSettlement := context.WithTimeout(ctx, 10*time.Second)
	if err := settlement.Err(); err != nil {
		stopSettlement()
		return false, err
	}
	settlementState, err := c.Repository.SettleArchiveGenerations(settlement, *ticket)
	settlementErr := settlement.Err()
	stopSettlement()
	if err != nil {
		return false, err
	}
	if settlementErr != nil {
		return false, settlementErr
	}
	if settlementState == "blocked" {
		return false, errors.New("archive generation tenant purge settlement refused")
	}
	if settlementState == "more" {
		checkpoint, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := checkpoint.Err(); err != nil {
			return false, err
		}
		accepted, err := c.Repository.Checkpoint(checkpoint, *ticket)
		if err != nil {
			return false, err
		}
		if err := checkpoint.Err(); err != nil {
			return false, err
		}
		if !accepted {
			return false, ErrTenantDeletionLeaseLost
		}
		return accepted, nil
	}
	if settlementState != "complete" {
		return false, errors.New("invalid archive generation settlement result")
	}
	if err := c.renewLease(ctx, *ticket); err != nil {
		return false, err
	}
	// Settlement is a separate SQL transition. Recheck every physical bucket
	// after it, before the irreversible metadata deletion.
	verify, stopVerify := context.WithTimeout(ctx, 40*time.Second)
	more, err = c.Objects.PurgeTenantBatch(verify, ticket.TenantID, c.Batch)
	verifyErr := verify.Err()
	stopVerify()
	if err != nil {
		return false, err
	}
	if verifyErr != nil {
		return false, verifyErr
	}
	if more {
		checkpoint, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := checkpoint.Err(); err != nil {
			return false, err
		}
		accepted, err := c.Repository.Checkpoint(checkpoint, *ticket)
		if err != nil {
			return false, err
		}
		if err := checkpoint.Err(); err != nil {
			return false, err
		}
		if !accepted {
			return false, ErrTenantDeletionLeaseLost
		}
		return true, nil
	}
	commit, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := commit.Err(); err != nil {
		return false, err
	}
	accepted, err := c.Repository.Finish(commit, *ticket)
	if err != nil {
		return false, err
	}
	if err := commit.Err(); err != nil {
		return false, err
	}
	if !accepted {
		return false, ErrTenantDeletionLeaseLost
	}
	return accepted, nil
}
