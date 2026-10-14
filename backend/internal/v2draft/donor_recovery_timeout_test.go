package v2draft

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"testing"
	"time"
)

type donorDeadlineDB struct {
	t        *testing.T
	expected time.Time
	calls    int
}

func (d *donorDeadlineDB) Begin(ctx context.Context) (pgx.Tx, error) {
	d.calls++
	deadline, ok := ctx.Deadline()
	if !ok || deadline.After(d.expected) || time.Until(deadline) <= 0 {
		d.t.Fatal("unbounded recovery Begin")
	}
	return nil, errors.New("database unavailable")
}
func TestDonorRecoveryReadsBoundDatabaseWait(t *testing.T) {
	for _, shorter := range []bool{false, true} {
		for _, name := range []string{"intent", "receipt", "descriptor"} {
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				expected := time.Now().Add(16 * time.Second)
				if shorter {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, time.Second)
					defer cancel()
					expected, _ = ctx.Deadline()
				}
				db := &donorDeadlineDB{t: t, expected: expected}
				who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
				tenant, run := uuid.New(), uuid.New()
				var raw []byte
				var err error
				switch name {
				case "intent":
					raw, err = (SQLDonorParamsPublisher{DB: db}).ReadIntent(ctx, who, tenant, run)
				case "receipt":
					raw, err = (SQLDonorDescriptorStore{DB: db}).ReadQueuedReceipt(ctx, who, tenant, run)
				case "descriptor":
					raw, err = (SQLDonorDescriptorStore{DB: db}).Read(ctx, who, tenant, run)
				}
				if err == nil || raw != nil || db.calls != 1 {
					t.Fatal("failed Begin returned recovery evidence")
				}
			})
		}
	}
}

type donorCleanupTx struct {
	pgx.Tx
	check func(context.Context)
}

func (t donorCleanupTx) Rollback(ctx context.Context) error {
	t.check(ctx)
	return errors.New("cleanup connection lost")
}

func TestDonorRollbackSurvivesCancellationWithOwnDeadline(t *testing.T) {
	type key struct{}
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "trace"))
	cancel()
	var retained context.Context
	rollbackDonorRecovery(parent, donorCleanupTx{check: func(ctx context.Context) {
		retained = ctx
		deadline, ok := ctx.Deadline()
		if ctx.Err() != nil || !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 5*time.Second || ctx.Value(key{}) != "trace" {
			t.Fatal("cleanup inherits cancellation or lacks bounded deadline")
		}
	}})
	if retained == nil || !errors.Is(retained.Err(), context.Canceled) {
		t.Fatal("cleanup context not released")
	}
}
