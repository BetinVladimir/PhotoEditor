package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestCreatePersonPreparationCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if worked, err := (TransferDraftPreparer{}).prepareMissingCreatePerson(ctx, auth.Identity{}, Request{}, "", PublishedIntent{}); worked || err != context.Canceled {
		t.Fatal("cancelled public preparation reached IO", err)
	}
	if worked, err := (TransferDraftPreparer{}).prepareMissingCreatePerson(context.Background(), auth.Identity{}, Request{}, "", PublishedIntent{}); worked || err == nil {
		t.Fatal("invalid request reached person preparation")
	}
}

func TestCreatePersonBudgetHonorsActualReservation(t *testing.T) {
	capacity := nativeRecoveryFactoryCapacityFixture().Capacity
	capacity.Output = 1 << 30
	got, err := createPersonBudget(capacity, 256<<20, v2qualification.Metrics{PeakVRAMBytes: 128 << 20})
	if err != nil || got.Output != 248<<20 || got.Reserved != 256<<20 || got.Costs.VRAMBytes != 128<<20 || capacity.Output != 1<<30 {
		t.Fatal("public reservation ceiling changed data or costs", err)
	}
	for _, reserved := range []int64{0, 8 << 20, -1, 9007199254740992} {
		if value, err := createPersonBudget(capacity, reserved, v2qualification.Metrics{}); err == nil || value.Output != 0 {
			t.Fatal("invalid reservation accepted", reserved)
		}
	}
	capacity.Output = 9007199254740992
	if value, err := createPersonBudget(capacity, 256<<20, v2qualification.Metrics{}); err == nil || value.Output != 0 {
		t.Fatal("invalid deployment output hidden by reservation bound")
	}
}
