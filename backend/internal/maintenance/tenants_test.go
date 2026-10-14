package maintenance

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type deniedTenantTransitionDB struct{}

func (deniedTenantTransitionDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("cancelled tenant transition reached SQL")
}

type tenantRepo struct {
	ticket                      *TenantDeletion
	checkpointed, settled, done bool
	refuseSettlement            bool
	refuseCheckpoint            bool
	refuseFinish                bool
	settlementMore              bool
	refuseRenewalAt             int
	renewed                     bool
	renewCalls                  int
	cancelOnClaim               context.CancelFunc
	cancelOnCheckpoint          context.CancelFunc
	cancelOnFinish              context.CancelFunc
}

func (r *tenantRepo) Claim(context.Context) (*TenantDeletion, error) {
	if r.cancelOnClaim != nil {
		r.cancelOnClaim()
	}
	return r.ticket, nil
}
func (r *tenantRepo) Checkpoint(context.Context, TenantDeletion) (bool, error) {
	r.checkpointed = true
	if r.cancelOnCheckpoint != nil { r.cancelOnCheckpoint() }
	return !r.refuseCheckpoint, nil
}
func (r *tenantRepo) SettleArchiveGenerations(context.Context, TenantDeletion) (string, error) {
	r.settled = true
	if r.refuseSettlement {
		return "blocked", nil
	}
	if r.settlementMore {
		return "more", nil
	}
	return "complete", nil
}
func (r *tenantRepo) RenewLease(context.Context, TenantDeletion) (bool, error) {
	r.renewed = true
	r.renewCalls++
	return r.renewCalls != r.refuseRenewalAt, nil
}
func (r *tenantRepo) Finish(context.Context, TenantDeletion) (bool, error) {
	r.done = true
	if r.cancelOnFinish != nil { r.cancelOnFinish() }
	return !r.refuseFinish, nil
}

type tenantObjects struct {
	more       bool
	err        error
	calls      int
	cancel     context.CancelFunc
	secondMore bool
	secondErr  error
}

func (o *tenantObjects) PurgeTenantBatch(context.Context, uuid.UUID, int) (bool, error) {
	o.calls++
	if o.cancel != nil {
		o.cancel()
	}
	if o.calls == 2 {
		return o.secondMore, o.secondErr
	}
	return o.more, o.err
}

func TestTenantCleanerCheckpointsUntilPrefixIsEmpty(t *testing.T) {
	for _, more := range []bool{true, false} {
		r := &tenantRepo{ticket: &TenantDeletion{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New()}}
		o := &tenantObjects{more: more}
		changed, err := (TenantCleaner{Repository: r, Objects: o, Batch: 64}).CleanOne(context.Background())
		expectedCalls := 2
		if more {
			expectedCalls = 1
		}
		if err != nil || !changed || o.calls != expectedCalls || r.checkpointed != more || r.settled == more || r.done == more {
			t.Fatalf("invalid tenant cleanup transition: more=%v checkpoint=%v done=%v err=%v", more, r.checkpointed, r.done, err)
		}
	}
}

func TestTenantCleanerRechecksPhysicalStorageAfterArchiveSettlement(t *testing.T) {
	for _, fail := range []bool{false, true} {
		r := &tenantRepo{ticket: &TenantDeletion{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New()}}
		o := &tenantObjects{secondMore: true}
		if fail {
			o.secondErr = errors.New("second physical scan failed")
		}
		changed, err := (TenantCleaner{Repository: r, Objects: o, Batch: 64}).CleanOne(context.Background())
		if o.calls != 2 || !r.settled || r.done {
			t.Fatal("finished without physical recheck", o.calls, r, err)
		}
		if fail {
			if changed || err == nil || r.checkpointed {
				t.Fatal("failed second scan advanced deletion", changed, err)
			}
		} else if err != nil || !changed || !r.checkpointed {
			t.Fatal("late remnant was not checkpointed", changed, err)
		}
	}
}

func TestTenantCleanerSecondScanCheckpointRequiresLiveLease(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "refused", true: "cancelled"}[cancelled], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := &tenantRepo{ticket: &TenantDeletion{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New()}}
			if cancelled {
				r.cancelOnCheckpoint = cancel
			} else {
				r.refuseCheckpoint = true
			}
			o := &tenantObjects{secondMore: true}
			changed, err := (TenantCleaner{Repository: r, Objects: o, Batch: 64}).CleanOne(ctx)
			want := error(ErrTenantDeletionLeaseLost)
			if cancelled {
				want = context.Canceled
			}
			if changed || !errors.Is(err, want) || !r.checkpointed || r.done || o.calls != 2 {
				t.Fatal("second scan advanced deletion after lease loss", changed, err, r, o)
			}
		})
	}
}

func TestTenantCleanerRefusesExpiredLeaseBeforePhysicalScans(t *testing.T) {
	for _, refusedAt := range []int{1, 2} {
		r := &tenantRepo{ticket: &TenantDeletion{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New()}, refuseRenewalAt: refusedAt}
		o := &tenantObjects{}
		changed, err := (TenantCleaner{Repository: r, Objects: o, Batch: 64}).CleanOne(context.Background())
		if changed || err == nil || !r.renewed || r.done || r.checkpointed || r.renewCalls != refusedAt || o.calls != refusedAt-1 {
			t.Fatal("expired lease reached physical scan or finish", refusedAt, changed, err, r, o)
		}
	}
}

func TestTenantCleanerRefusesFinishWhenArchiveGenerationSettlementFails(t *testing.T) {
	r := &tenantRepo{ticket: &TenantDeletion{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New()}, refuseSettlement: true}
	changed, err := (TenantCleaner{Repository: r, Objects: &tenantObjects{}, Batch: 64}).CleanOne(context.Background())
	if changed || err == nil || !r.settled || r.done || r.checkpointed {
		t.Fatal("tenant deletion bypassed archive generation settlement", changed, err)
	}
}

func TestTenantCleanerCheckpointsBoundedArchiveGenerationSettlement(t *testing.T) {
	r := &tenantRepo{ticket: &TenantDeletion{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New()}, settlementMore: true}
	changed, err := (TenantCleaner{Repository: r, Objects: &tenantObjects{}, Batch: 64}).CleanOne(context.Background())
	if err != nil || !changed || !r.settled || !r.checkpointed || r.done {
		t.Fatal("bounded archive settlement did not checkpoint", changed, err)
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

func TestTenantCleanerRefusesTransitionWhenStorageIgnoresCancellation(t *testing.T) {
	for _, more := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		r := &tenantRepo{ticket: &TenantDeletion{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New()}}
		o := &tenantObjects{more: more, cancel: cancel}
		changed, err := (TenantCleaner{Repository: r, Objects: o, Batch: 64}).CleanOne(ctx)
		if changed || !errors.Is(err, context.Canceled) || r.done || r.checkpointed || o.calls != 1 {
			t.Fatal("cancelled storage observation advanced irreversible tenant deletion", more, changed, err, r, o)
		}
	}
}

func TestTenantCleanerRefusesClaimThatReturnsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &tenantRepo{ticket: &TenantDeletion{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New()}, cancelOnClaim: cancel}
	o := &tenantObjects{}
	changed, err := (TenantCleaner{Repository: r, Objects: o, Batch: 64}).CleanOne(ctx)
	if changed || !errors.Is(err, context.Canceled) || o.calls != 0 || r.done || r.checkpointed {
		t.Fatal("late claim began irreversible cleanup", changed, err, r, o)
	}
}

func TestTenantCleanerRefusesSuccessAfterLateTransitionCancellation(t *testing.T) {
	for _, mode := range []string{"checkpoint", "finish"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := &tenantRepo{ticket: &TenantDeletion{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New()}}
			o := &tenantObjects{more: mode == "checkpoint"}
			if mode == "checkpoint" { r.cancelOnCheckpoint = cancel } else { r.cancelOnFinish = cancel }
			changed, err := (TenantCleaner{Repository: r, Objects: o, Batch: 64}).CleanOne(ctx)
			if changed || !errors.Is(err, context.Canceled) || (mode == "checkpoint" && !r.checkpointed) || (mode == "finish" && !r.done) {
				t.Fatal("cancelled transition reported success", mode, changed, err, r)
			}
		})
	}
}

func TestTenantCleanerSurfacesRefusedTransitionAsLeaseLoss(t *testing.T) {
	for _, mode := range []string{"checkpoint", "finish"} {
		t.Run(mode, func(t *testing.T) {
			r := &tenantRepo{ticket: &TenantDeletion{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New()}}
			o := &tenantObjects{more: mode == "checkpoint"}
			if mode == "checkpoint" { r.refuseCheckpoint = true } else { r.refuseFinish = true }
			changed, err := (TenantCleaner{Repository: r, Objects: o, Batch: 64}).CleanOne(context.Background())
			if changed || !errors.Is(err, ErrTenantDeletionLeaseLost) {
				t.Fatal("refused tenant transition treated as idle", mode, changed, err)
			}
		})
	}
}

func TestSQLTenantDeletionRefusesCancelledTransitionBeforeQuery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := SQLTenantDeletion{DB: deniedTenantTransitionDB{}}
	ticket := TenantDeletion{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New()}
	for _, transition := range []func(context.Context, TenantDeletion) (bool, error){service.Checkpoint, service.Finish} {
		accepted, err := transition(ctx, ticket)
		if accepted || !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled transition reached SQL", accepted, err)
		}
	}
	if state, err := service.SettleArchiveGenerations(ctx, ticket); state != "" || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled settlement reached SQL", state, err)
	}
	if renewed, err := service.RenewLease(ctx, ticket); renewed || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled renewal reached SQL", renewed, err)
	}
}
