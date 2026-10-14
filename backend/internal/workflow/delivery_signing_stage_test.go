package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/delivery"
)

type expiredPolicyDB struct{}

func (expiredPolicyDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("expired policy stage performed other SQL")
}

type expiredPolicyObservations struct{}

func (expiredPolicyObservations) Renew(context.Context, delivery.ExpansionLease) error {
	panic("expired policy stage renewed after terminal failure")
}

func (expiredPolicyObservations) ReadObservationPage(context.Context, delivery.ExpansionLease, delivery.PreparePlan, int, int) ([]delivery.ChildObservation, error) {
	panic("expired policy stage scanned signer results")
}

type expiredPolicyFinalizer struct{ calls int }

func (f *expiredPolicyFinalizer) FailExpiredPolicy(context.Context, delivery.ExpansionLease, delivery.PreparePlan) (bool, error) {
	f.calls++
	return true, nil
}

func TestDeliverySigningStageRequiresInstalledClosure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (DeliverySigningStage{}).ProcessSigning(ctx,
		delivery.ExpansionLease{}, delivery.PreparePlan{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled stage accepted", err)
	}
	if _, err := (DeliverySigningStage{}).ProcessSigning(context.Background(),
		delivery.ExpansionLease{}, delivery.PreparePlan{}); err == nil {
		t.Fatal("unconfigured stage accepted")
	}
}

func TestExpiredSigningPolicyFailureRequiresBoundScheduler(t *testing.T) {
	store := SQLDeliverySignedFailureStore{}
	if _, err := store.FailExpiredPolicy(context.Background(), delivery.ExpansionLease{}, delivery.PreparePlan{}); err == nil {
		t.Fatal("uninstalled expiry finalizer accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store.Instance = uuid.New()
	if _, err := store.FailExpiredPolicy(ctx, delivery.ExpansionLease{}, delivery.PreparePlan{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled expiry finalizer continued", err)
	}
}

func TestSigningStageStopsAfterExpiredPolicyFinalization(t *testing.T) {
	plan, parent, _, _, _ := deliveryExportPlanFixture(t, nil)
	lease := delivery.ExpansionLease{Expansion: uuid.New(), Token: uuid.New(),
		Until: time.Now().Add(time.Minute), Tenant: uuid.New(), Run: parent,
		Workflow: uuid.New(), Package: uuid.New(), ParentTask: plan.TaskID(),
		Cursor: 1, SlotCount: 1}
	finalizer := new(expiredPolicyFinalizer)
	stage := DeliverySigningStage{Store: delivery.ExpansionLeaseStore{DB: expiredPolicyDB{}},
		Observations: expiredPolicyObservations{}, ExpiredPolicy: finalizer, PageSize: 1}
	state, err := stage.ProcessSigning(context.Background(), lease, plan)
	if err != nil || state != "finalized_failed" || finalizer.calls != 1 {
		t.Fatal("expired policy did not terminate signing before dispatch", state, err, finalizer.calls)
	}
}
