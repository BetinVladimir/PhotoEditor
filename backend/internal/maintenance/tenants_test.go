package maintenance

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type tenantRepo struct {
	ticket             *TenantDeletion
	checkpointed, done bool
}

func (r *tenantRepo) Claim(context.Context) (*TenantDeletion, error) { return r.ticket, nil }
func (r *tenantRepo) Checkpoint(context.Context, TenantDeletion) (bool, error) {
	r.checkpointed = true
	return true, nil
}
func (r *tenantRepo) Finish(context.Context, TenantDeletion) (bool, error) {
	r.done = true
	return true, nil
}

type tenantObjects struct {
	more  bool
	err   error
	calls int
}

func (o *tenantObjects) PurgeTenantBatch(context.Context, uuid.UUID, int) (bool, error) {
	o.calls++
	return o.more, o.err
}

func TestTenantCleanerCheckpointsUntilPrefixIsEmpty(t *testing.T) {
	for _, more := range []bool{true, false} {
		r := &tenantRepo{ticket: &TenantDeletion{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New()}}
		o := &tenantObjects{more: more}
		changed, err := (TenantCleaner{Repository: r, Objects: o, Batch: 64}).CleanOne(context.Background())
		if err != nil || !changed || o.calls != 1 || r.checkpointed != more || r.done == more {
			t.Fatalf("invalid tenant cleanup transition: more=%v checkpoint=%v done=%v err=%v", more, r.checkpointed, r.done, err)
		}
	}
}

func TestTenantCleanerNeverCommitsFailedOrInvalidPurge(t *testing.T) {
	r := &tenantRepo{ticket: &TenantDeletion{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New()}}
	o := &tenantObjects{err: errors.New("storage unavailable")}
	if changed, err := (TenantCleaner{Repository: r, Objects: o, Batch: 64}).CleanOne(context.Background()); err == nil || changed || r.done || r.checkpointed {
		t.Fatal("failed purge advanced tenant deletion")
	}
	if _, err := (TenantCleaner{Repository: r, Objects: o, Batch: 0}).CleanOne(context.Background()); err == nil {
		t.Fatal("invalid batch accepted")
	}
}
